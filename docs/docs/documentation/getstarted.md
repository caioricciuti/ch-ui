---
sidebar_position: 1
layout: default
title: 🚀 Get Started
---

### What you need?

- **ClickHouse**: You need to have a ClickHouse instance running. You can download it from [here](https://clickhouse.com/docs/en/install).
- **Docker**: You'll need Docker to run CH-UI. You can download it for [_Server Linux here_](https://docs.docker.com/engine/install/ubuntu/) or for [_Desktop here_](https://www.docker.com/products/docker-desktop/)

### How to run CH-UI?

Option 1: **Build from source**

1. **Clone the repository**: You can clone the repository using the following command:

```bash
git clone https://github.com/caioricciuti/ch-ui.git
```

2. **Install dependencies**: Navigate to the project directory and install the dependencies:

```bash
cd ch-ui
cd client && npm install
cd server && npm install
```