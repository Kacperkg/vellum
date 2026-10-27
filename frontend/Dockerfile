FROM node:24.22.1-alpine3.24@sha256:06d7adf3e0ef61a13f1d28682726366f97a15aa1888c934d3ecec86c33918b53

WORKDIR /app

RUN npm install --global npm@11.21.1 \
    && npm install --global pnpm@10.34.8

COPY . .

RUN chmod +x docker-dev.sh

EXPOSE 5173

CMD ["./docker-dev.sh"]