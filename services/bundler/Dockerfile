# Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
# Proprietary. See LICENSE. Provenance: VAPOR-6eabb1be532bdef4
#
# Alto bundler image (GPL: @pimlico/alto; see THIRD_PARTY_NOTICES). The Alto
# source is not modified — this only pins the version and our launcher.
FROM node:22-bookworm-slim
WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*
COPY package.json ./
RUN npm install --omit=dev --no-audit --no-fund
COPY alto.config.json start.sh ./
RUN chmod +x start.sh && useradd -m alto
USER alto
EXPOSE 4337
ENTRYPOINT ["./start.sh"]
