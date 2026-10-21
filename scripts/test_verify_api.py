"""Offline fixtures for the redacted acceptance helper; never calls the API."""
import importlib.util, json, pathlib, unittest

spec = importlib.util.spec_from_file_location('verify_api', pathlib.Path(__file__).with_name('verify-api.py'))
api = importlib.util.module_from_spec(spec)
spec.loader.exec_module(api)

class AcceptanceFixtures(unittest.TestCase):
    def test_incomplete_success_is_not_completion(self):
        self.assertFalse(api.completed('responses', b'{"status":"incomplete"}'))
        self.assertFalse(api.completed('messages', b'{"type":"message","stop_reason":"max_tokens"}'))
        self.assertFalse(api.completed('chat', b'{"choices":[{"finish_reason":"length"}]}'))

    def test_stream_requires_terminal_and_no_error(self):
        self.assertTrue(api.completed('responses', b'data: {"type":"response.completed"}\n\n', stream=True))
        self.assertFalse(api.completed('responses', b'data: {"type":"response.created"}\n\n', stream=True))
        self.assertFalse(api.completed('chat', b'data: {"error":{"message":"private"}}\n\ndata: [DONE]\n', stream=True))

    def test_diagnostic_tool_is_selected_without_execution(self):
        body = api.payload('responses', 'model-id', tool=True)
        self.assertEqual(body['tool_choice']['name'], 'phases_probe')
        self.assertFalse(body['store'])
        self.assertTrue(api.completed('responses', json.dumps({'output':[{'type':'function_call','name':'phases_probe'}]}).encode(), tool=True))
        self.assertFalse(api.completed('responses', b'{"output":[]}', tool=True))

    def test_redirects_are_not_followed(self):
        self.assertIsNone(api.NoRedirect().redirect_request(None, None, 302, 'redirect', {}, 'https://elsewhere.invalid'))

if __name__ == '__main__':
    unittest.main()
