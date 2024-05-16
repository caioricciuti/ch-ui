MIT License

Copyright (c) 2024 Caio Ricciuti

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

## Acknowledgments

This project makes use of several open-source packages, greatly simplifying its development. We acknowledge and are grateful to these developers for their contributions:

## Core Dependencies and main tool

- **Click House**: An open-source column-oriented database management system. It is capable of real-time generation of analytical data reports using SQL queries.
  - `@clickhouse` 

### UI Components & Styles

- **Radix UI**: A comprehensive suite of UI primitives for building high-quality, accessible design systems and web apps.
  - React AlertDialog, Collapsible, Context Menu, Dialog, Label, Popover, Scroll Area, Select, Separator, Slot, Tabs, Toast, Tooltip components (`@radix-ui/react-*`)
- **ag-Grid**: Highly performant and feature-rich grid library.
  - `ag-grid-community`, `ag-grid-react`
- **Monaco Editor**: Browser-based code editor that powers VS Code.
  - `monaco-editor`, `@monaco-editor/react`
- **Echarts**: A powerful charting and visualization library.
  - `echarts`, `echarts-for-react`
- **Framer Motion**: A library for React to create powerful animations.
  - `framer-motion`
- **Tailwind CSS**: A utility-first CSS framework for rapid UI development.
  - `tailwindcss-animate`, `tailwind-merge`
- **Lucide React**: Beautifully designed icon library in React.
  - `lucide-react`
- **CMDK**: Command menu toolkit for building command-driven interfaces.
  - `cmdk`
- **React Tabs**: Accessible and easy-to-use tab components for React apps.
  - `react-tabs`
  - **shadcn/ui**: Without a doubt, the most beautiful and accessible UI components for React.
  - `@shadcn/ui`

### Utilities

- **Class Variance Authority & clsx**: Utilities for conditionally joining classNames together.
  - `class-variance-authority`, `clsx`
- **React Resizable Panels**: React component for resizable panel group layouts.
  - `react-resizable-panels`
- **SQL Formatter**: A library for pretty-printing SQL queries.
  - `sql-formatter`
- **Sonner**: Toast notifications library to provide feedback.
  - `sonner`
- **Vaul**: Lightweight library to manage local state in React.
  - `vaul`

### React Ecosystem

- **React**: A JavaScript library for building user interfaces.
  - `react`, `react-dom`
- **React Router**: Declarative routing for React.
  - `react-router-dom`

This project is licensed under the MIT License - see the [LICENSE.md](LICENSE) file for details.
