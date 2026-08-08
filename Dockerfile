# ---- build stage ----
FROM golang:1.25-alpine AS build
WORKDIR /src

# cache deps first
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Pure-Go SQLite (modernc) => CGO off => fully static binary.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath -ldflags="-s -w" \
    -o /out/apptracker ./cmd/apptracker

# Prepare a /data dir with nonroot ownership so a fresh named volume
# inherits it (Docker seeds empty named volumes from the image).
RUN mkdir -p /out/data && chown -R 65532:65532 /out/data

# ---- runtime stage ----
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=build /out/apptracker /apptracker
COPY --from=build --chown=65532:65532 /out/data /data

USER nonroot:nonroot
EXPOSE 8080
ENV PORT=8080 \
    DB_PATH=/data/apptracker.db
VOLUME ["/data"]

ENTRYPOINT ["/apptracker"]
