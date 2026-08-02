# Canary: every line here is a deliberate hadolint / trivy-misconfig trigger.
FROM ubuntu:latest
RUN apt-get update && apt-get install -y curl
ADD http://example.com/payload.tar.gz /app/payload.tar.gz
USER root
CMD /app/run.sh
