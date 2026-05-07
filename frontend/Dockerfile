FROM node:22-alpine AS build

WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY index.html vite.config.js ./
COPY src ./src
ARG VITE_API_BASE=/api
ARG VITE_WS_PATH=/ws
ARG VITE_WS_URL=
ENV VITE_API_BASE=$VITE_API_BASE
ENV VITE_WS_PATH=$VITE_WS_PATH
ENV VITE_WS_URL=$VITE_WS_URL
RUN npm run build

FROM node:22-alpine

WORKDIR /app
COPY --from=build /src/dist ./dist
COPY server.mjs healthcheck.mjs ./
USER node

EXPOSE 8080
CMD ["node", "/app/server.mjs"]

