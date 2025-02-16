// Hero Section Component

import { type FC } from "react";
import Link from "@docusaurus/Link";
import { ArrowBigDown } from "lucide-react";
import clsx from "clsx";
import DotPattern from "./ui/dotpattern";
import Logo from "@site/static/img/logo.png"

interface HeroSectionProps {
  className?: string;
}

export const HeroSection: FC<HeroSectionProps> = ({ className }) => {
  const scrollToContent = () => {
    window.scrollTo({
      top: window.innerHeight - 10,
      behavior: "smooth",
    });
  };

  return (
    <>
      <DotPattern />

      <section className={clsx("hero-section", className)}>
        <div className="hero-container">
          <div className="hero-content">
            <h1 className="sr-only">CH-UI</h1>
            <p className="hero-title">Data is better when we see it!</p>
            <p className="hero-description">
              CH-UI makes working with data easy. This UI connects you directly
              to your ClickHouse instance, allowing you to view, filter, and
              export your data with ease.
            </p>

            <div className="flex flex-wrap gap-2 items-center">
              {/* License */}
              <img
                src="https://img.shields.io/github/license/caioricciuti/ch-ui"
                alt="License"
                loading="lazy"
              />

              {/* Version */}
              <img
                src="https://img.shields.io/github/package-json/v/caioricciuti/ch-ui"
                alt="Version"
                loading="lazy"
              />

              {/* Last Commit */}
              <img
                src="https://img.shields.io/github/last-commit/caioricciuti/ch-ui"
                alt="Last Commit"
                loading="lazy"
              />

              {/* Issues */}
              <img
                src="https://img.shields.io/github/issues/caioricciuti/ch-ui"
                alt="Issues"
                loading="lazy"
              />

              {/* Pull Requests */}
              <img
                src="https://img.shields.io/github/issues-pr/caioricciuti/ch-ui"
                alt="Pull Requests"
                loading="lazy"
              />
            </div>

            <div className="hero-buttons">
              <Link href="/docs" className="hero-button">
                Get Started 🚀
              </Link>

              <Link
                href="https://github.com/caioricciuti/ch-ui"
                target="_blank"
                rel="noopener noreferrer"
                className="github-stars"
              >
                <img
                  src="https://img.shields.io/github/stars/caioricciuti/ch-ui?style=social"
                  alt="GitHub Stars"
                  loading="lazy"
                />
              </Link>
            </div>
          </div>

          <div className="hero-image-container">
            <div className="hero-image-wrapper">
              <img
                src={Logo}
                alt="ch-UI Logo"
                className="hero-image"
                loading="lazy"
              />
            </div>
          </div>
        </div>

        <button
          onClick={scrollToContent}
          className="scroll-button"
          aria-label="Scroll to content"
        >
          <ArrowBigDown className="scroll-arrow" size={58} />
        </button>
      </section>
    </>
  );
};

export default HeroSection;
