FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY bin/tedge-container-monitor /usr/bin
ENV MQTT_HOST="host.docker.internal:1883"
ENTRYPOINT [ "/usr/bin/tedge-container-monitor" ]
CMD ["run"]