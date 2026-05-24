ARG BASE_IMAGE=scratch
ARG ARCH=$TARGETARCH
####################################################################################################
# base
####################################################################################################
FROM alpine:3.17 AS base
ARG ARCH
RUN apk update && apk upgrade && \
    apk add ca-certificates && \
    apk --no-cache add tzdata

COPY dist/kynomesh-linux-${ARCH} /bin/kynomesh

RUN chmod +x /bin/kynomesh

####################################################################################################
# kynomesh
####################################################################################################
ARG BASE_IMAGE
FROM ${BASE_IMAGE} AS kynomesh
ARG ARCH

COPY --from=base /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=base /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=base /bin/kynomesh /bin/kynomesh

ENTRYPOINT ["/bin/kynomesh"]