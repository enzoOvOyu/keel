This empty read-only mount keeps platform manifest signing disabled by default.
Never put keys here. Provision an owner-only key directory outside the repository
and set KEEL_MEDIA_SIGNING_DIRECTORY and KEEL_MEDIA_SIGNING_KEY_FILE as documented
in docs/DEPLOYMENT.md.
