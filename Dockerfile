FROM alpine:3.20
RUN apk --no-cache add ca-certificates
ARG TARGETARCH
COPY dist/kuberush-linux-${TARGETARCH} /usr/local/bin/kuberush
RUN chmod +x /usr/local/bin/kuberush
EXPOSE 8080
ENTRYPOINT ["kuberush"]
