# syntax=docker/dockerfile:1.7

ARG NODE_IMAGE=node:20.19.5-bookworm-slim@sha256:9e70124bd00f47dd023e349cd587132ae61892acc0e47ed641416c3e18f401c3
ARG GO_IMAGE=golang:1.24.13-alpine3.23@sha256:8bee1901f1e530bfb4a7850aa7a479d17ae3a18beb6e09064ed54cfd245b7191
ARG RUNTIME_IMAGE=alpine:3.22.5@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce
# Next.js/webpack 模块 ID 依赖绝对构建路径；CI 用宿主工作区路径构建以保证
# docker 通道与 host 通道产物逐字节一致（默认 /build 保持不变）。
ARG WEB_BUILD_DIR=/build

FROM ${NODE_IMAGE} AS web-builder

ARG VERSION=dev
ARG COMMIT=unknown
ARG WEB_BUILD_DIR
ENV HUSKY=0
WORKDIR ${WEB_BUILD_DIR}
COPY ./web/package.json ./web/pnpm-lock.yaml ./
RUN corepack enable && pnpm install --frozen-lockfile
COPY ./web ./
RUN NEXT_BUILD_ID="$COMMIT" NEXT_PUBLIC_APP_VERSION="$VERSION" pnpm build:embedded

FROM ${NODE_IMAGE} AS zashboard-builder

ARG COMMIT=unknown
ENV HUSKY=0
WORKDIR /build
COPY ./zashboard/package.json ./zashboard/pnpm-lock.yaml ./
RUN corepack enable && pnpm install --frozen-lockfile
COPY ./zashboard ./
RUN ZASHBOARD_COMMIT_ID="$COMMIT" pnpm build:embedded

FROM scratch AS frontend-assets
ARG WEB_BUILD_DIR
COPY --from=web-builder ${WEB_BUILD_DIR}/build /web/build
COPY --from=zashboard-builder /build/dist /zashboard/dist

FROM ${GO_IMAGE} AS server-builder

ARG VERSION=dev
ARG WEB_BUILD_DIR
ENV CGO_ENABLED=0 GOOS=linux
WORKDIR /build
COPY ./go.mod ./go.sum ./
RUN go mod download && go mod verify
COPY . .
RUN rm -rf ./web/build ./zashboard/dist
COPY --from=web-builder ${WEB_BUILD_DIR}/build ./web/build
COPY --from=zashboard-builder /build/dist ./zashboard/dist
RUN go test -tags embedassets -run '^TestProductionBuildEmbedsBothFrontends$' -count=1 . && \
    go build -tags embedassets -trimpath -buildvcs=false \
      -ldflags "-s -w -X poolx/internal/pkg/common.Version=$VERSION" \
      -o /out/poolnext-server .

FROM ${RUNTIME_IMAGE} AS runtime

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S -g 10001 poolnext && \
    adduser -S -D -H -u 10001 -G poolnext poolnext && \
    install -d -o poolnext -g poolnext -m 0750 /data

ENV PORT=3000 \
    GIN_MODE=release \
    SQLITE_PATH=/data/poolnext.db \
    UPLOAD_PATH=/data/upload \
    POOLNEXT_BOOTSTRAP_FILE=/data/poolnext-bootstrap.txt
COPY --from=server-builder --chown=root:root /out/poolnext-server /usr/local/bin/poolnext-server
USER 10001:10001
WORKDIR /data
EXPOSE 3000
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=5 \
  CMD wget -q -O /dev/null http://127.0.0.1:3000/api/status || exit 1
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/local/bin/poolnext-server"]
