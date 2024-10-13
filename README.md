# CH-UI

CH-UI is a modern and feature-rich user interface for ClickHouse databases. It offers an intuitive platform for managing ClickHouse databases, executing queries, and visualizing metrics about your instance. Built with React and ClickHouse client for web, CH-UI aims to streamline the use of ClickHouse databases and provide a more user-friendly experience for developers and data engineers.

![GitHub license](https://img.shields.io/github/license/caioricciuti/ch-ui)

## Features

- **ClickHouse Integration**: Seamlessly interact with ClickHouse databases, manage connections, and execute queries.
- **Dynamic UI Components**: Utilize advanced UI components for enhanced data interaction.
- **Responsive Tab Management**: Create, manage, and dynamically interact with various tabs like query tabs and table tabs.
- **Performance Optimizations**: Efficient state management and optimized database interactions - Uses indexedDB for caching.
- **TypeScript Refactor**: 🚀 Fully refactored codebase using TypeScript for improved code quality and developer experience.
- **Enhanced Metrics**: 📊 Re-build metrics dashboard with new insights and visualization options using scopes such as Queries, Tables, Settings, Network and more for better usage.
- **Custom Table Management**: 🔧 Tables are now created and handled internally, removing dependency on 3rd party packages.
- **SQL Editor IntelliSense**: 💡 Improved SQL editing experience with autocomplete suggestions and syntax highlighting.
- **Intuitive Data Explorer**: 🔍 Redesigned interface for easier navigation and data manipulation.
- **Fresh New Design**: 🎨 Modern, clean UI overhaul for improved usability and aesthetics.

### Screenshots

<img src="./public/screen-shots/settings.png" alt="Screenshot of the application" width="300">
<img src="./public/screen-shots/main-page.png" alt="Screenshot of the application" width="300">
<img src="./public/screen-shots/instance-metrics.png" alt="Screenshot of the application" width="200">

## Getting Started

### Using npm and building from scratch

```bash
git clone https://github.com/caioricciuti/ch-ui.git
cd ch-ui
npm install
npm run build

** preview **
npm run preview

** debug **
npm run dev
```

### Using Docker

```bash
docker run -p 5521:5521 ghcr.io/caioricciuti/ch-ui:latest
```

### Using Environment Variables with Docker

CH-UI now supports setting ClickHouse connection details using environment variables when running with Docker. You can use the following variables:

VITE_CLICKHOUSE_URL: The URL of your ClickHouse server
VITE_CLICKHOUSE_USER: The username for ClickHouse authentication
VITE_CLICKHOUSE_PASS: The password for ClickHouse authentication

Example:

```bash
docker run -p 5521:5521 \
  -e VITE_CLICKHOUSE_URL=http://your-clickhouse-server:8123 \
  -e VITE_CLICKHOUSE_USER=your-username \
  -e VITE_CLICKHOUSE_PASS=your-password \
  ghcr.io/caioricciuti/ch-ui:latest
```

When these environment variables are set, CH-UI will automatically use them to configure the ClickHouse connection, bypassing the need for manual setup in the UI.

### Using docker-compose to run a localhost clickhouse (only for development)

The following command will run a clickhouse database with a connection to http://localhost:8123 and a user `dev` with password `dev`.
The data will be persisted in this directory: `.clickhouse_local_data`

```bash
docker-compose -f docker-compose-dev.yml up -d
```

Use the following command to turn down, don't forget to remove `.clickhouse_local_data` if you want to save some space in your HD but then all data will be erased.

```bash
docker-compose -f docker-compose-dev.yml down
```

### Prerequisites

Only for building from scratch: (if you are using docker you don't need to install these).

```bash
nodejs >= 20.x
npm >= 10.x
```

## Documentation

For information on how to use CH-UI, please refer to our documentation:

- [Getting Started](https://ch-ui.caioricciuti.com/docs/getting-started?utm_source=ch-ui&utm_medium=gitHubReadme)

## Security Recommendations

I recommend using a reverse proxy with authentication, as the app itself only caches and makes data available on the browser; we don't have a backend (yet). If you plan to make CH-UI available for your team on the open internet, it's good practice to use basic authentication or run it on a private network. You can use Nginx, Apache, or any other reverse proxy that supports basic authentication. Here is an example of how to set up basic authentication with Nginx:

```nginx
server {
    listen 80;
    server_name your-server-name;

    location / {
        proxy_pass http://localhost:5521;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        auth_basic "Restricted Access";
        auth_basic_user_file /path/to/.htpasswd;
    }
}
```

To create the `.htpasswd` file for basic authentication:

1. Install the `apache2-utils` package (on Ubuntu/Debian) or `httpd-tools` (on CentOS/RHEL):

   ```
   sudo apt-get install apache2-utils
   ```

   or

   ```
   sudo yum install httpd-tools
   ```

2. Create the `.htpasswd` file and add a user:

   ```
   sudo htpasswd -c /path/to/.htpasswd username
   ```

   Replace `/path/to/.htpasswd` with the actual path where you want to store the file, and `username` with the desired username.

3. You'll be prompted to enter and confirm a password for the user.

Remember to replace `your-server-name` with your actual domain name or IP address, and adjust the `proxy_pass` URL if your application is running on a different port or host.

### Additional Security Measures

1. **Use HTTPS**: It's highly recommended to use HTTPS to encrypt all traffic. You can obtain a free SSL certificate from Let's Encrypt and configure Nginx to use it.

2. **IP Whitelisting**: If your team works from fixed IP addresses, you can add an additional layer of security by whitelisting these IPs in your Nginx configuration.

3. **Regular Updates**: Keep your Nginx server and all dependencies up to date to ensure you have the latest security patches.

4. **Firewall**: Configure a firewall (like `ufw` on Ubuntu) to only allow necessary incoming connections.

By implementing these security measures, you can significantly reduce the risk of unauthorized access to your CH-UI instance. Always follow the principle of least privilege and regularly review and update your security configurations.

Also remember that the most important credential you have is the connection information to your ClickHouse server, that is what gives access to your data. CH-UI does not manage users, it's done on your ClickHouse instance, read the [ClickHouse documentation to learn how to manage users and permissions.](https://clickhouse.com/docs/en/cloud/security/common-access-management-queries?utm_source=ch-ui&utm_medium=gitHubReadme)

## Limitations

Since the tabs and it's results are cached on the browser using IndexedDB, if you have a lot of data or a lot of tabs open, it can consume a lot of memory on the browser, this makes the app to be quite freeze, I'm working on a solution to this, but for now, I recommend to use the app with a reasonable amount of data and tabs open.

## Authors and Contributors

This project is maintained by Caio Ricciuti and it gets better and better with all contributions from the community. If you want to contribute, please refer to the [Contribution Guidelines](https://ch-ui.caioricciuti.com/docs/contributing?utm_source=ch-ui&utm_medium=gitHubReadme).

### Contributors

<a href="https://github.com/caioricciuti/ch-ui/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=caioricciuti/ch-ui" />
</a>

Made with contrib.rocks

## License

This project is licensed under the MIT License - see the LICENSE.md file for details

## Disclaimer

This project is not affiliated with ClickHouse or Yandex. It is an independent project developed by Caio Ricciuti to provide a modern and user-friendly interface for ClickHouse databases. For official ClickHouse documentation and support, please visit the [ClickHouse website](https://clickhouse.com/docs/en/?utm_source=ch-ui&utm_medium=gitHubReadme).

## Support

Finally, if you like this project and want to support it:

#### Financially: [![Buy me a coffee](https://img.shields.io/badge/Buy%20me%20a%20coffee-Donate-FF813F.svg)](https://www.buymeacoffee.com/caioricciuti).

#### Contributing ![GitHub issues](https://img.shields.io/github/issues/caioricciuti/ch-ui) find a bug or suggest a feature, all lines of code are welcome! 🚀

#### Recognition: ⭐️ the project on GitHub, share it with your friends, or spread the word, it helps a lot!

All contributions are greatly appreciated and help to keep the project running and improving. Thank you for your support!


### [acknowledgments and Credits](https://ch-ui.caioricciuti.com/docs/acknowledgments?utm_source=ch-ui&utm_medium=gitHubReadme)