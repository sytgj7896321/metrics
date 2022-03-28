FROM golang:alpine as builder1

RUN apk update && apk add --no-cache ca-certificates && update-ca-certificates

FROM golang:1.17 as builder2

ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64 \
    GOPROXY='https://proxy.golang.com.cn,direct'

WORKDIR /build

RUN git init && \
    git remote add -f origin https://github.com/sytgj7896321/metrics.git && \
    git config core.sparse-checkout true && \
    echo serviceQuotasExporter >> .git/info/sparse-checkout && \
    git reset --hard origin/test

RUN cd serviceQuotasExporter && go build .

FROM scratch

LABEL version="1.0.0" maintainer=sytgj7896321@gmail.com

COPY --from=builder1 /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=builder2 /build/serviceQuotasExporter /

ENTRYPOINT ["/serviceQuotasExporter"]
