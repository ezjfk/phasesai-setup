"""Run opt-in, potentially billable production API checks without logging a key or reply text."""
import argparse, datetime, getpass, json, os, pathlib, re, sys, urllib.error, urllib.parse, urllib.request

ORIGIN = 'https://api.phasesai.cc'
class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs): return None
opener = urllib.request.build_opener(NoRedirect)

def call(path, key='', payload=None):
    headers = {'User-Agent': 'PhasesAI-Acceptance/1', 'Content-Type': 'application/json'}
    if key: headers['Authorization'] = 'Bearer ' + key
    if path == '/v1/messages': headers['anthropic-version'] = '2023-06-01'
    request = urllib.request.Request(ORIGIN + path, headers=headers, data=None if payload is None else json.dumps(payload).encode())
    try: response = opener.open(request, timeout=90)
    except urllib.error.HTTPError as error: response = error
    data = response.read(2 * 1024 * 1024 + 1)
    if len(data) > 2 * 1024 * 1024: raise RuntimeError('Response limit exceeded')
    request_id = response.headers.get('x-request-id', '')
    if not re.fullmatch(r'[a-fA-F0-9-]{16,64}', request_id): request_id = ''
    cost = response.headers.get('x-phases-cost-usd', '')
    if not re.fullmatch(r'\d+(?:\.\d+)?', cost): cost = ''
    safe = {'status': response.status, 'request_id': request_id, 'cost_header_usd': cost}
    return safe, data

def catalog():
    entries = []; cursor = ''; seen = set()
    for _ in range(100):
        safe, body = call('/v1/models?limit=20' + ('&after_id=' + urllib.parse.quote(cursor, safe='') if cursor else ''))
        if safe['status'] != 200: raise RuntimeError('Catalog request failed: HTTP ' + str(safe['status']))
        data = json.loads(body); entries += data['data']
        if not data['has_more']: return entries
        cursor = data.get('last_id', '')
        if not cursor or cursor in seen: raise RuntimeError('Pagination did not advance')
        seen.add(cursor)
    raise RuntimeError('Catalog pagination exceeded limit')

def payload(protocol, model, stream=False, tool=False):
    if protocol == 'messages':
        body = {'model': model, 'max_tokens': 64, 'stream': stream, 'messages': [{'role': 'user', 'content': 'Reply with OK.'}]}
        if tool:
            body['messages'][0]['content'] = 'Call phases_probe with value true.'
            body['tools'] = [{'name': 'phases_probe', 'description': 'A harmless diagnostic with no external actions.', 'input_schema': {'type':'object','properties':{'value':{'type':'boolean'}},'required':['value']}}]
            body['tool_choice'] = {'type':'tool','name':'phases_probe'}
        return body
    if protocol == 'responses':
        body = {'model': model, 'max_output_tokens':64, 'store':False, 'stream':stream, 'input':'Reply with OK.'}
        if tool:
            body['input'] = 'Call phases_probe with value true.'
            body['tools'] = [{'type':'function','name':'phases_probe','description':'A harmless diagnostic with no external actions.','parameters':{'type':'object','properties':{'value':{'type':'boolean'}},'required':['value'],'additionalProperties':False}}]
            body['tool_choice'] = {'type':'function','name':'phases_probe'}
        return body
    body = {'model':model,'max_tokens':64,'stream':stream,'messages':[{'role':'user','content':'Reply with OK.'}]}
    if tool:
        body['messages'][0]['content'] = 'Call phases_probe with value true.'
        body['tools'] = [{'type':'function','function':{'name':'phases_probe','description':'A harmless diagnostic with no external actions.','parameters':{'type':'object','properties':{'value':{'type':'boolean'}},'required':['value']}}}]
        body['tool_choice'] = {'type':'function','function':{'name':'phases_probe'}}
    return body

def completed(protocol, body, stream=False, tool=False):
    if stream:
        events = []
        for line in body.decode('utf-8').splitlines():
            if line.startswith('data:'):
                value = line[5:].strip()
                if value == '[DONE]': events.append({'type':'done'}); continue
                try: events.append(json.loads(value))
                except json.JSONDecodeError: continue
        if any(value.get('type') in ['error','response.failed','response.incomplete'] or value.get('error') for value in events): return False
        terminal = {'messages':'message_stop','responses':'response.completed','chat':'done'}[protocol]
        return any(value.get('type') == terminal for value in events)
    value = json.loads(body)
    if value.get('error'): return False
    if protocol == 'messages':
        return any(x.get('type') == 'tool_use' and x.get('name') == 'phases_probe' for x in value.get('content', [])) if tool else value.get('type') == 'message' and value.get('stop_reason') == 'end_turn'
    if protocol == 'responses':
        return any(x.get('type') == 'function_call' and x.get('name') == 'phases_probe' for x in value.get('output', [])) if tool else value.get('status') == 'completed'
    choices = value.get('choices', [])
    return any(x.get('function',{}).get('name') == 'phases_probe' for choice in choices for x in choice.get('message',{}).get('tool_calls',[])) if tool else bool(choices) and choices[0].get('finish_reason') == 'stop'

def main():
    parser = argparse.ArgumentParser(); parser.add_argument('--output', default='phasesai-acceptance.json'); args = parser.parse_args()
    models = catalog(); ids = {value['id'] for value in models}
    print('Public catalog verified:', len(ids), 'model IDs. No credential used for discovery.')
    print('This test can send NINE small billable requests (64 output-token ceilings). No tools execute; a tool-selection response is inspected only.')
    print('Use a dedicated, spend-limited test key. No key or model response text is saved.')
    if input('Type RUN to continue, or Enter to cancel: ').strip() != 'RUN': return 1
    key = os.environ.get('PHASESAI_API_KEY') or getpass.getpass('PhasesAI test key (hidden): ')
    if not key or '\r' in key or '\n' in key: raise RuntimeError('Invalid credential format')
    report = {'time':datetime.datetime.now(datetime.timezone.utc).isoformat(),'public_catalog_count':len(ids),'api_checks':[], 'harness_execution_verified':False,'usage_dashboard_verified':False}
    bad, _ = call('/v1/responses', 'invalid-phasesai-diagnostic-key', {'model':models[0]['id'],'input':'OK','max_output_tokens':1,'store':False})
    report['invalid_key_status'] = bad['status']
    for protocol, endpoint in [('messages','/v1/messages'),('responses','/v1/responses'),('chat','/v1/chat/completions')]:
        model = input(protocol + ' model ID (Enter to skip): ').strip()
        if not model: continue
        if model not in ids: raise RuntimeError('Choose an exact ID from the catalog')
        for mode in ['completion','stream','tool_selection']:
            try:
                safe, body = call(endpoint,key,payload(protocol,model,stream=mode=='stream',tool=mode=='tool_selection'))
                safe['passed'] = 200 <= safe['status'] < 300 and completed(protocol,body,stream=mode=='stream',tool=mode=='tool_selection')
            except Exception as error:
                # Never serialize exception strings: transport errors can carry private data.
                safe = {'passed':False,'error_kind':type(error).__name__}
            safe.update({'protocol':protocol,'mode':mode,'model':model});report['api_checks'].append(safe)
            print(protocol,mode,'PASS' if safe['passed'] else 'FAIL',safe.get('status',''))
    path = pathlib.Path(args.output); path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(report,indent=2)+'\n',encoding='utf-8')
    print('Redacted report:',path)
    return 0 if report['api_checks'] and all(check['passed'] for check in report['api_checks']) else 1

if __name__ == '__main__':
    try: sys.exit(main())
    except KeyboardInterrupt: sys.exit(130)
    except Exception as error:
        print('Acceptance stopped:',type(error).__name__,file=sys.stderr);sys.exit(1)
