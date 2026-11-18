FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/praxis . \
    && mkdir /out/data

FROM gcr.io/distroless/static-debian13:nonroot
WORKDIR /app
COPY --from=build /out/praxis /app/praxis
COPY seed.json /app/seed.json
COPY --from=build --chown=65532:65532 /out/data /data
ENV PRAXIS_ADDR=0.0.0.0:8080 \
    PRAXIS_DB=/data/praxis.db \
    PRAXIS_SEED_FILE=/app/seed.json
EXPOSE 8080
VOLUME /data
USER nonroot:nonroot
ENTRYPOINT ["/app/praxis"]
