# Use an official Node runtime as a parent image
FROM node:20-alpine as build

# Set the working directory
WORKDIR /app

# Copy package.json and package-lock.json (or yarn.lock)
COPY package*.json ./

# Install dependencies
RUN npm install

# Run browser list update
RUN npx update-browserslist-db@latest


# Bundle app source inside Docker image
COPY . .

# Build the app
RUN npm run build

# Use a second stage to reduce image size
FROM node:20-alpine

# Set the working directory for the second stage
WORKDIR /app

# Install 'serve' to serve the app on port 5521
RUN npm install -g serve

# Copy the build directory from the first stage to the second stage
COPY --from=build /app/dist /app

# Copy a script to inject environment variables
COPY inject-env.js /app/inject-env.js

# Expose port 5521 to have it mapped by the Docker daemon
EXPOSE 5521

# Define environment variables
ENV VITE_CLICKHOUSE_URL=""
ENV VITE_CLICKHOUSE_USER=""
ENV VITE_CLICKHOUSE_PASS=""
ENV VITE_CLICKHOUSE_USE_ADVANCED=""
ENV VITE_CLICKHOUSE_CUSTOM_PATH=""

RUN addgroup -S ch-group -g 1001 && adduser -S ch-user -u 1001 -G ch-group

RUN chown -R ch-user:ch-group /app

# Use a shell script to inject environment variables and then serve the app
CMD ["/bin/sh", "-c", "node inject-env.js && serve -s -l 5521"]