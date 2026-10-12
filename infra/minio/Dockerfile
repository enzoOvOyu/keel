# Development only: official community release, built from checksum-verified source.
FROM golang:1.27.2-bookworm AS build
ENV GOTOOLCHAIN=local CGO_ENABLED=0
WORKDIR /src
RUN curl -fsSL https://codeload.github.com/minio/minio/tar.gz/9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a -o /tmp/minio.tgz \
 && echo "45521908307306e925c98d629e1c17d78c8b72b6ee242b1bfb1409f7d8ee5841  /tmp/minio.tgz" | sha256sum -c - \
 && tar -xzf /tmp/minio.tgz --strip-components=1 -C /src \
 && rm /tmp/minio.tgz
RUN go build -p 2 -trimpath -ldflags="-s -w" -o /out/minio .
FROM debian:bookworm-slim
LABEL org.opencontainers.image.source="https://github.com/minio/minio" org.opencontainers.image.revision="9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a"
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl \
 && rm -rf /var/lib/apt/lists/* \
 && mkdir /data && chown 65532:65532 /data
COPY --from=build /out/minio /usr/local/bin/minio
COPY --from=build /src/LICENSE /src/NOTICE /src/CREDITS /usr/share/licenses/minio/
USER 65532:65532
EXPOSE 9000 9001
HEALTHCHECK --interval=10s --timeout=5s --start-period=30s --retries=6 CMD curl -fsS http://127.0.0.1:9000/minio/health/live || exit 1
ENTRYPOINT ["minio"]
CMD ["server", "/data", "--console-address", ":9001", "--certs-dir", "/tmp/certs"]
