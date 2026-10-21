import manifest from './manifest.mjs';

const PUBLIC_HOST = 'downloads.phasesai.cc';
const MAX_BYTES = 20 * 1024 * 1024;

export async function serveArtifact(request, env, entry, fetchAsset = fetch) {
  let object = await env.RELEASES.get(entry.key);
  if (object && object.customMetadata?.sha256 !== entry.sha256) {
    // Immutable keys are never silently repaired or replaced.
    return new Response('Artifact metadata mismatch', { status: 503 });
  }
  if (!object) {
    const upstream = await fetchAsset(entry.source, { redirect: 'follow', signal: AbortSignal.timeout(45000), headers: { 'User-Agent': 'PhasesAI-Setup-Distribution/1' } });
    if (!upstream.ok) {
      console.error(JSON.stringify({ event: 'release_origin_failed', artifact: entry.filename, status: upstream.status }));
      return new Response(`Release temporarily unavailable (origin HTTP ${upstream.status})`, { status: 502 });
    }
    const finalURL = new URL(upstream.url || entry.source);
    if (finalURL.protocol !== 'https:' || !(finalURL.hostname === 'github.com' || finalURL.hostname.endsWith('.githubusercontent.com'))) {
      return new Response('Unexpected release origin', { status: 502 });
    }
    if (Number(upstream.headers.get('content-length') || 0) > MAX_BYTES) return new Response('Artifact too large', { status: 502 });
    const reader = upstream.body.getReader();
    const chunks = []; let size = 0;
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      size += value.length;
      if (size > MAX_BYTES) { await reader.cancel(); return new Response('Artifact too large', { status: 502 }); }
      chunks.push(value);
    }
    const bytes = new Uint8Array(size); let offset = 0;
    for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.length; }
    const hash = [...new Uint8Array(await crypto.subtle.digest('SHA-256', bytes))].map(x => x.toString(16).padStart(2, '0')).join('');
    if (hash !== entry.sha256) return new Response('Release integrity check failed', { status: 502 });
    await env.RELEASES.put(entry.key, bytes, {
      sha256: entry.sha256,
      onlyIf: { etagDoesNotMatch: '*' },
      httpMetadata: { contentType: 'application/octet-stream', cacheControl: 'public, max-age=31536000, immutable' },
      customMetadata: { sha256: entry.sha256, repository: manifest.repository, sourceCommit: entry.sourceCommit || manifest.sourceCommit },
    });
    object = await env.RELEASES.get(entry.key);
    if (!object || object.customMetadata?.sha256 !== entry.sha256) return new Response('Artifact storage unavailable', { status: 503 });
  }
  return new Response(request.method === 'HEAD' ? null : object.body, {
    headers: {
      'Content-Type': 'application/octet-stream',
      'Content-Length': String(object.size),
      'Content-Disposition': `attachment; filename="${entry.filename}"`,
      'Cache-Control': 'public, max-age=31536000, immutable',
      'X-Content-Type-Options': 'nosniff',
      'X-Artifact-SHA256': entry.sha256,
      'X-Artifact-Source-Commit': entry.sourceCommit || manifest.sourceCommit,
      'Accept-Ranges': 'none',
    },
  });
}

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    if (url.hostname !== PUBLIC_HOST) return new Response('Not found', { status: 404 });
    if (!['GET', 'HEAD'].includes(request.method)) return new Response('Method not allowed', { status: 405, headers: { Allow: 'GET, HEAD' } });
    if (url.pathname === '/setup/preview.json') {
      return new Response(request.method === 'HEAD' ? null : JSON.stringify(manifest), { headers: { 'Content-Type': 'application/json', 'Cache-Control': 'no-store', 'X-Content-Type-Options': 'nosniff' } });
    }
    // A stable channel must be explicitly promoted after release acceptance.
    if (url.pathname === '/setup/stable.json') return new Response(JSON.stringify({ status: 'preview_only', repository: manifest.repository }), { status: 404, headers: { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' } });
    const entry = manifest.assets[url.pathname];
    if (!entry) return new Response('Not found', { status: 404 });
    const withdrawn = manifest.withdrawn || (manifest.withdrawnVersions || []).some(tag => url.pathname.startsWith(`/setup/${tag}/`));
    if (withdrawn && /^phasesai-setup-(darwin|linux|windows)-(amd64|arm64)(\.exe)?$/.test(entry.filename)) {
      return new Response('This executable preview was withdrawn. See /setup/preview.json for the current release.', { status: 410, headers: { 'Cache-Control': 'no-store' } });
    }
    try { return await serveArtifact(request, env, entry); }
    catch { console.error(JSON.stringify({ event: 'release_delivery_failed', artifact: entry.filename })); return new Response('Download temporarily unavailable', { status: 503 }); }
  },
};
