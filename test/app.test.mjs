import test from 'node:test';
import assert from 'node:assert/strict';
import { route, createHandler } from '../src/app.mjs';

test('health identifies the deployed revision', () => {
  assert.deepEqual(route('GET', '/healthz', 'abc123').body,
    { status: 'ok', service: 'keel', version: 'abc123' });
});
test('unknown endpoints do not silently succeed', () => {
  assert.equal(route('GET', '/api/v1/deployments').status, 404);
});
test('write requests are refused while foundation has no authentication', () => {
  for (const method of ['POST', 'DELETE', 'PUT', 'PATCH']) {
    assert.equal(route(method, '/healthz').status, 405);
  }
});
test('HEAD returns headers without a body; query strings do not affect routing', () => {
  let status, headers, body;
  createHandler('test')({ method: 'HEAD', url: '/healthz?probe=1' }, {
    writeHead(s, h) { status = s; headers = h; },
    end(b) { body = b; },
  });
  assert.equal(status, 200);
  assert.equal(headers['Cache-Control'], 'no-store');
  assert.equal(body, undefined);
});
