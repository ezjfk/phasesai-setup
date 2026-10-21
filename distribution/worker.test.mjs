import test from 'node:test';
import assert from 'node:assert/strict';
import worker, { serveArtifact } from './worker.mjs';
import manifest from './manifest.mjs';

const bytes = new TextEncoder().encode('public release fixture');
const sha256 = [...new Uint8Array(await crypto.subtle.digest('SHA-256', bytes))].map(x => x.toString(16).padStart(2, '0')).join('');
const entry = { key: 'setup/v1/fixture', filename: 'fixture', source: 'https://github.com/ezjfk/phasesai-setup/releases/download/v1/fixture', sha256 };
function bucket() {
  const objects = new Map(); let writes = 0;
  return { get writes() { return writes; }, async get(key) { const o = objects.get(key); return o ? { ...o, body: new Response(o.bytes).body } : null; }, async put(key, data, options) { writes++; assert.equal(options.onlyIf.etagDoesNotMatch, '*'); if (!objects.has(key)) objects.set(key, { bytes: data, size: data.length, customMetadata: options.customMetadata }); } };
}

test('release download verifies bytes before storing and reuses the immutable R2 object', async () => {
  const RELEASES = bucket(); let fetches = 0;
  const fetcher = async () => { fetches++; return new Response(bytes); };
  for (const method of ['GET', 'HEAD']) {
    const response = await serveArtifact(new Request('https://downloads.phasesai.cc/setup/v1/fixture', { method }), { RELEASES }, entry, fetcher);
    assert.equal(response.status, 200); assert.equal(response.headers.get('X-Artifact-SHA256'), sha256);
    if (method === 'GET') assert.equal(await response.text(), 'public release fixture'); else assert.equal(await response.text(), '');
  }
  assert.equal(fetches, 1); assert.equal(RELEASES.writes, 1);
});

test('corrupt release bytes never reach R2 or the customer', async () => {
  const RELEASES = bucket();
  const response = await serveArtifact(new Request('https://downloads.phasesai.cc/setup/v1/fixture'), { RELEASES }, entry, async () => new Response('corrupt executable'));
  assert.equal(response.status, 502); assert.equal(RELEASES.writes, 0); assert.match(await response.text(), /integrity/);
});

test('unknown hosts, paths, methods and an unpromoted stable channel are refused', async () => {
  for (const request of [new Request('https://other.invalid/'),new Request('https://downloads.phasesai.cc/unknown'),new Request('https://downloads.phasesai.cc/setup/stable.json')]) assert.equal((await worker.fetch(request, {})).status, 404);
  assert.equal((await worker.fetch(new Request('https://downloads.phasesai.cc/', { method: 'POST' }), {})).status, 405);
});

test('a withdrawn native executable cannot be downloaded from the R2 host', async () => {
  const previous = manifest.withdrawn;
  manifest.withdrawn = true;
  try {
    const path = Object.keys(manifest.assets).find(path => path.endsWith('windows-amd64.exe'));
    const response = await worker.fetch(new Request('https://downloads.phasesai.cc' + path), {});
    assert.equal(response.status, 410);
    assert.equal(response.headers.get('Cache-Control'), 'no-store');
  } finally { manifest.withdrawn = previous; }
});

test('publishing a new preview never re-enables a withdrawn version', async () => {
  const previous = manifest.withdrawn;
  manifest.withdrawn = false;
  try {
    const old = Object.keys(manifest.assets).find(path => path.includes('/v0.0.0-preview.20261021163405/') && path.endsWith('windows-amd64.exe'));
    assert.ok(old);
    assert.equal((await worker.fetch(new Request('https://downloads.phasesai.cc' + old), {})).status, 410);
  } finally { manifest.withdrawn = previous; }
});
