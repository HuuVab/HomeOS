# syntax=docker/dockerfile:1

FROM node:24-alpine3.23 AS frontend-build
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.27-alpine3.23 AS backend-build
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/homeos ./cmd/homeos

FROM alpine:3.23
RUN addgroup -S homeos && adduser -S -G homeos homeos && mkdir -p /data && chown homeos:homeos /data
WORKDIR /app
COPY --from=backend-build /out/homeos /app/homeos
COPY --from=frontend-build /src/frontend/dist /app/web
ENV HOMEOS_DATA_DIR=/data HOMEOS_WEB_DIR=/app/web HOMEOS_BIND_ADDR=0.0.0.0
USER homeos
EXPOSE 8080
CMD ["/app/homeos"]
