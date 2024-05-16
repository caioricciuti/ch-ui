# Use an official Node runtime as a parent image
FROM node:20-alpine as build

# Set the working directory
WORKDIR /app

# Copy package.json and package-lock.json (or yarn.lock)
COPY package*.json ./

# Install dependencies
RUN npm install

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

# Expose port 5521 to have it mapped by the Docker daemon
EXPOSE 5521

# Define the command to run the app using a static server
CMD ["serve", "-s", "-l", "5521"]