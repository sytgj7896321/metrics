FROM golang:1.17 as builder

ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64 \
    GOPROXY='https://proxy.golang.com.cn,direct'

WORKDIR /build

COPY ./ ./

RUN go build -o serviceQuotasExporter ./

FROM scratch

LABEL version="1.0.0" maintainer=sytgj7896321@gmail.com

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=builder /build/serviceQuotasExporter /

ENTRYPOINT ["/serviceQuotasExporter"]
