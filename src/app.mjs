export function route(method, pathname, version = 'development') {
  if (method !== 'GET' && method !== 'HEAD') {
    return { status: 405, headers: { Allow: 'GET, HEAD' }, body: { error: 'method_not_allowed' } };
  }
  if (pathname === '/healthz') {
    return { status: 200, body: { status: 'ok', service: 'keel', version } };
  }
  if (pathname === '/api/v1') {
    return { status: 200, body: { service: 'keel', version, stage: 'foundation' } };
  }
  return { status: 404, body: { error: 'not_found' } };
}

export function createHandler(version) {
  return (req, res) => {
    const started = Date.now();
    let result;
    try {
      const pathname = new URL(req.url, 'http://localhost').pathname;
      result = route(req.method, pathname, version);
    } catch {
      result = { status: 400, body: { error: 'bad_request' } };
    }
    res.writeHead(result.status, {
      'Content-Type': 'application/json; charset=utf-8',
      'Cache-Control': 'no-store',
      'X-Content-Type-Options': 'nosniff',
      ...result.headers,
    });
    res.end(req.method === 'HEAD' ? undefined : JSON.stringify(result.body));
    console.log(JSON.stringify({ time: new Date().toISOString(), method: req.method,
      status: result.status, durationMs: Date.now() - started }));
  };
}
