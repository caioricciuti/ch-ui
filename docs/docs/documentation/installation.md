---
sidebar: false
title: ⌨️ Installation
---

## 🏃 How to run CH-UI?

### 🐳 **Docker** (_Recommended_)

1. **Download run.sh**: Download the `run.sh` script from the repository:

```bash
curl -O https://raw.githubusercontent.com/caioricciuti/ch-ui/main/run.sh
```

2. **Run the script**: Run the script to start CH-UI:

```bash
chmod +x run.sh ## Make the script executable (if needed).
./run.sh ## Run the script. -d flag to run in detached mode.
```

### 💿 **Build from source**

1. **Clone the repository**: You can clone the repository using the following command:

```bash
git clone https://github.com/caioricciuti/ch-ui.git
```

2. **Install dependencies**: Navigate to server and client project directory and install the dependencies:

Server:

```bash
cd ch-ui
cd server && npm install
node index.js
```

Client:

```bash
cd ch-ui
cd client && npm install
npm run build
npm run preview
```

3. **Access CH-UI**: Open your browser and navigate to `http://localhost:5173`.
