# Multi-stage Dockerfile for CH-UI
# Supports both npm and Bun package managers

# Build stage
FROM node:20-alpine AS build

# Build arguments
ARG VERSION=dev
ARG COMMIT_SHA=unknown
ARG BUILD_DATE

# Set the working directory
WORKDIR /app

# Copy package files
COPY package*.json ./
COPY bun.lock* ./

# Install dependencies
# Check if bun.lock exists to determine package manager
RUN if [ -f "bun.lock" ]; then \
      echo "Using Bun for dependency installation" && \
      npm install -g bun && \
      bun install --frozen-lockfile; \
    else \
      echo "Using npm for dependency installation" && \
      npm ci --prefer-offline --no-audit; \
    fi

# Run browser list update
RUN npx update-browserslist-db@latest

# Copy application source
COPY . .

# Build the application
RUN if [ -f "bun.lock" ]; then \
      bun run build; \
    else \
      npm run build; \
    fi

# Runtime stage
FROM node:20-alpine AS runtime

# Install serve for serving static files
RUN npm install -g serve

# Set the working directory
WORKDIR /app

# Copy built application from build stage
COPY --from=build /app/dist /app

# Copy environment injection script
COPY inject-env.cjs /app/inject-env.cjs

# Create non-root user
RUN addgroup -S ch-group -g 1001 && \
    adduser -S ch-user -u 1001 -G ch-group

# Set ownership
RUN chown -R ch-user:ch-group /app

# Add metadata labels
LABEL org.opencontainers.image.title="CH-UI" \
      org.opencontainers.image.description="A modern web interface for ClickHouse databases" \
      org.opencontainers.image.vendor="Caio Ricciuti" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT_SHA}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.source="https://github.com/caioricciuti/ch-ui"

# Environment variables with defaults
ENV VITE_CLICKHOUSE_URL="" \
    VITE_CLICKHOUSE_USER="" \
    VITE_CLICKHOUSE_PASS="" \
    VITE_CLICKHOUSE_USE_ADVANCED="" \
    VITE_CLICKHOUSE_CUSTOM_PATH="" \
    VITE_CLICKHOUSE_REQUEST_TIMEOUT=30000 \
    VITE_BASE_PATH="/" \
    NODE_ENV=production

# Expose port
EXPOSE 5521

# Switch to non-root user
USER ch-user

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:5521 || exit 1

# Start the application
CMD ["/bin/sh", "-c", "node inject-env.cjs && serve -s -l 5521"]