FROM alpine:3.21

RUN adduser -D -h /home/mnemos mnemos
COPY mnemos /usr/local/bin/

USER mnemos
WORKDIR /home/mnemos

ENTRYPOINT ["mnemos"]
