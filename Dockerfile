FROM golang:1.24-bookworm AS build

WORKDIR /app

RUN apt-get update \
	&& apt-get install -y --no-install-recommends libolm-dev \
	&& rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 go build -o /out/easymatrix ./cmd/server

FROM debian:bookworm-slim

RUN apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates libolm3 \
	&& rm -rf /var/lib/apt/lists/* \
	&& mkdir -p /data/gomuks

WORKDIR /data

COPY --from=build /out/easymatrix /usr/local/bin/easymatrix

ENV GOMUKS_ROOT=/data/gomuks

EXPOSE 8080

CMD ["easymatrix"]
