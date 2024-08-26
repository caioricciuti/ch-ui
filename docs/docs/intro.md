---
sidebar_position: 1
title: Discover CH-UI
---

import { FeatureCard, FeatureGrid } from '@site/src/components/FeatureCard';

# 🧑‍🚀️ Discover CH-UI

**CH-UI** is your gateway to effortless data management and insightful analytics with ClickHouse. Let's explore how it can help you and your organization to reach new heights in data-driven decision-making.

## 🚀 What CH-UI Does

CH-UI makes your life and data projects **easy** to manage and maintain when you need privacy and control over your data.

#### Here's what you can do:

<FeatureGrid>
  <FeatureCard
    title="ClickHouse Integration"
    description="Seamlessly interact with your ClickHouse instances through an intuitive user interface."
    icon="database"
  />
  <FeatureCard
    title="Results Visualization"
    description="See the results of your queries on the go in filtrable tables"
    icon="table"
  />
  <FeatureCard
    title="Query Editor"
    description="Write and execute SQL queries with ease using our powerful editor."
    icon="code"
  />
  <FeatureCard
    title="Data Management"
    description="Effortlessly manage tables and databases within your ClickHouse instance."
    icon="file"
  />
  <FeatureCard
    title="User Management"
    description="Control access with customizable roles and permissions."
    icon="users"
  />
  <FeatureCard
    title="Data Import/Export"
    description="Export the results of your queries"
    icon="exchange-alt"
  />
  <FeatureCard
    title="Data Transformation"
    description="Transform your data using the power of SQL, right within CH-UI."
    icon="magic"
  />
  <FeatureCard
    title="Metrics"
    description="Keep an eye on your instance with a metrics dashboard."
    icon="chart-line"
  />
    <FeatureCard
    title="It's yours"
    description="All your data managed by you, spin a Click House instance, connect with CH-UI and relax, it's on the house"
    icon="network-wired"
  />
</FeatureGrid>

And much more! CH-UI is designed to be your go-to tool for all things related to your data team! Finally you can focus on what matters most: your data, your insights, your decisions. Make your data team's life easier and more productive with **CH-UI**.

## 🤔 Why CH-UI?

[ClickHouse](https://clickhouse.com) is an incredibly **THE BEST** OLAP database, after years working with proprietary DB's I had no
idea that an open-source database could be so powerful, but it is, and it's amazing. The command line interface is great, but it's not for everyone, and that's where CH-UI comes in.

- [Check out Click House documentation](https://clickhouse.com/docs)

NOTE: This is not a Click House project, it's a personal project, I'm not affiliated with Click House in any way, I just love it.

## 🛠 How It Works

CH-UI is built on my best knowledge. It's a full-stack application that leverages the power of modern web technologies to deliver a seamless user experience.

### Client Side

- Built with **React** and **TypeScript**
- Uses **Zustand** for efficient state management
- Communicates with the server via **REST API**

### Server Side

- Powered by **Node.js** and **Express**
- Handles authentication and authorization with **JWT**
- Connects to ClickHouse using the official [ClickHouse JS Client](https://clickhouse.com/docs/en/integrations/language-clients/javascript)
- Manages data operations and user permissions

### Data Flow

1. User interacts with the CH-UI client
2. Client sends requests to the server via REST API
3. Server processes requests, interacting with ClickHouse as needed
4. Server sends data back to the client
5. Client displays data in a user-friendly format

## 💡 Why Choose CH-UI?

- **Open Source**: Customize and extend to fit your unique needs, your data, your way!
- **User-Friendly**: Intuitive interface for effortless data interaction
- **Powerful**: Harness the full potential of ClickHouse with enhanced UX
- **Community-Driven**: Shape the future of CH-UI with your contributions!

Ready to supercharge your ClickHouse experience? [Get started with CH-UI](/docs/documentation/getstarted) now!
