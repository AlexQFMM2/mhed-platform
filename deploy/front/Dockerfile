FROM node:24-alpine AS build

RUN corepack enable
WORKDIR /src
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY web/package.json web/package.json
COPY admin/package.json admin/package.json
RUN pnpm install --frozen-lockfile
COPY web/ web/
COPY admin/ admin/
RUN pnpm build

FROM nginx:1.28-alpine

COPY deploy/front/nginx.conf /etc/nginx/nginx.conf
COPY --from=build /src/web/dist /usr/share/nginx/web
COPY --from=build /src/admin/dist /usr/share/nginx/admin
EXPOSE 8080 8081
