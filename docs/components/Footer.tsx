"use client";

import React from "react";
import Link from "next/link";
import CustomLogo from "./CustomLogo";
import { Github } from "lucide-react";

export default function Footer() {
  return (
    <footer className="bg-muted py-12 px-4 md:px-6 lg:px-8 w-full">
      <div className="container mx-auto">
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-8 mb-12">
          <div className="flex flex-col items-start gap-4">
            <Link href="/" className="flex items-center gap-2" prefetch={false}>
              <CustomLogo />
            </Link>
            <p className="text-muted-foreground text-sm">
              Data is better when we see it!
            </p>
            <div className="flex items-center gap-4 mt-4">
              <Link
                href="https://github.com/caioricciuti/ch-ui"
                className="text-muted-foreground hover:text-primary transition-colors"
                prefetch={false}
              >
                <Github className="w-5 h-5" />
              </Link>
            </div>
          </div>
          <div className="grid gap-4">
            <h4 className="text-sm font-medium">Documentation</h4>
            <Link
              href="/docs/getting-started"
              className="text-sm hover:underline"
              prefetch={false}
            >
              Get Started
            </Link>
            <Link
              href="/docs/license"
              className="text-sm hover:underline"
              prefetch={false}
            >
              License
            </Link>
          </div>

          <div className="grid gap-4">
            <h4 className="text-sm font-medium">Community</h4>
            <Link
              href="https://github.com/caioricciuti/ch-ui/discussions"
              className="text-sm hover:underline"
              prefetch={false}
            >
              GitHub Discussions
            </Link>
            <Link href="#" className="text-sm hover:underline" prefetch={false}>
              Contributing
            </Link>
          </div>
        </div>
        <div className="border-t pt-8 flex flex-col md:flex-row justify-between items-center gap-4">
          <p className="text-xs text-muted-foreground">
            © {new Date().getFullYear()} CH-UI. All rights reserved.
          </p>
          <div className="flex gap-4 text-xs text-muted-foreground">
            <Link
              href="/legal/privacy-policy"
              className="hover:underline"
              prefetch={false}
            >
              Privacy Policy
            </Link>
            <Link
              href="/legal/terms-of-service"
              className="hover:underline"
              prefetch={false}
            >
              Terms of Service
            </Link>
          </div>
        </div>
      </div>
    </footer>
  );
}
