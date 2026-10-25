Agent enrollment and mTLS heartbeats remain disabled unless
KEEL_AGENT_IDENTITY_FILE is explicitly configured. This directory is the
read-only placeholder mount; never put CA keys, certificates or tokens here.

Operator configuration uses absolute runtime paths for listen,
server_certificate, server_key, ca_certificate and ca_key. Keep configuration
and private keys owned by the process UID with mode 0600, in a private operator
directory mounted read-only at /run/keel/agent. Public certificates must also be
owned by that UID and must not be group/world writable. The listener is separate
from the browser HTTP port; compose.yaml publishes no Agent port by default.

The binary currently supports identity enrollment and liveness only. It does not
install itself, accept task dispatch or report hardware health. Builds do not
constitute approved media or a G3+ installation authorization.
