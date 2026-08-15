# syntax=docker/dockerfile:1

FROM docker.m.daocloud.io/library/golang:1.26.3-bookworm AS builder
WORKDIR /src
ENV GOTOOLCHAIN=local \
    CGO_ENABLED=0 \
    GOPROXY=https://goproxy.cn,direct \
    GOSUMDB=sum.golang.google.cn
COPY . .
RUN go build -mod=vendor -o /out/walkv .

FROM docker.m.daocloud.io/library/alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /out/walkv /app/walkv
EXPOSE 8080
ENTRYPOINT ["/app/walkv"]
