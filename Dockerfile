FROM node:22-alpine
ARG APP_VERSION=development
ENV NODE_ENV=production HOST=0.0.0.0 PORT=3000 APP_VERSION=${APP_VERSION}
WORKDIR /app
COPY --chown=node:node package.json package-lock.json ./
COPY --chown=node:node src ./src
USER node
EXPOSE 3000
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
  CMD node -e "fetch('http://127.0.0.1:3000/healthz').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"
CMD ["node", "src/server.mjs"]
