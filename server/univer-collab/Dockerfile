# Univer collaboration server for WeKnora.
#
# Assembles the Univer Server SDK (transport, endpoint, service, PostgreSQL
# adapter) into one Node.js process. WeKnora reaches it through the app's
# reverse proxy; nothing else needs to be exposed publicly.
FROM node:22-alpine

WORKDIR /app

COPY package.json package-lock.json* ./
RUN npm install --omit=dev --no-audit --no-fund

COPY main.js ./

ENV NODE_ENV=production \
    UNIVER_HOST=0.0.0.0 \
    UNIVER_PORT=3010

EXPOSE 3010

CMD ["node", "main.js"]
