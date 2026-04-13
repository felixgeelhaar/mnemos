FROM golang:1.25-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

RUN CGO_ENABLED=0 go build \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildDate=${BUILD_DATE}" \
    -o /bin/mnemos ./cmd/mnemos && \
    CGO_ENABLED=0 go build \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildDate=${BUILD_DATE}" \
    -o /bin/mnemos-mcp ./cmd/mnemos-mcp

FROM alpine:3.21

RUN adduser -D -h /home/mnemos mnemos
COPY --from=builder /bin/mnemos /bin/mnemos-mcp /usr/local/bin/

USER mnemos
WORKDIR /home/mnemos

ENTRYPOINT ["mnemos"]
