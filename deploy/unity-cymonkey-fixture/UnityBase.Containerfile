# Private, operator-built Unity Editor base image.
# Build with /opt/unity as the context after installing the Linux Editor there:
#   docker build -f UnityBase.Containerfile -t private/unity-editor:2022.3 .
# The Editor and any Unity license remain private and are never published by
# Jangolova.
FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates libasound2 libfontconfig1 libgl1 libglu1-mesa \
      libgtk-3-0 libnss3 libpulse0 libx11-6 libxcursor1 libxcomposite1 \
      libxdamage1 libxext6 libxi6 libxinerama1 libxrandr2 xvfb \
    && rm -rf /var/lib/apt/lists/*

COPY . /opt/unity/
RUN test -x /opt/unity/Editor/Unity

WORKDIR /workspace
ENTRYPOINT ["/opt/unity/Editor/Unity"]
