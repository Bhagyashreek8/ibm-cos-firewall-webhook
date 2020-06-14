# FROM scratch
FROM registry.access.redhat.com/ubi8/ubi-minimal:8.2-267
LABEL maintainer="nkashyap@in.ibm.com"
COPY ./webhook-server /
ENTRYPOINT ["/webhook-server"]
