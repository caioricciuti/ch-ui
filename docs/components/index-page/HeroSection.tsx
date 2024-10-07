import React from "react";
import { Link } from "nextra-theme-docs";
import Image from "next/image";
import { RainbowButton } from "../ui/RainbowButton";
export const HeroSection = () => {
  return (
    <section className="w-full py-12 md:py-24 lg:py-32">
      <div className="container mx-auto grid items-center gap-6 px-4 md:px-6 lg:grid-cols-2 lg:gap-10">
        <div className="space-y-6">
          <h1 className="text-4xl font-extrabold tracking-tight sm:text-5xl md:text-6xl lg:text-7xl bg-clip-text text-transparent bg-gradient-to-r from-orange-600 to-pink-500">
            Data is better when we see it!
          </h1>
          <p className="max-w-[600px] md:text-xl/relaxed lg:text-xl/relaxed xl:text-2xl/relaxed">
            CH-UI makes working with data easy. Our tools help you uncover
            patterns and trends, making data-driven decisions simpler than ever.
          </p>
          <div className="flex flex-col gap-4 sm:flex-row sm:items-center">
            <Link
              href="/docs/"
              className="inline-flex items-center justify-center no-underline px-8 py-3 text-xl font-bold text-white rounded-md bg-gradient-to-r from-orange-600 to-orange-300 hover:from-orange-500 hover:to-orange-200 transition-all duration-300 ease-in-out transform hover:scale-105"
            >
              Get Started 🚀
            </Link>
          </div>
          <RainbowButton>Sponor Us 🌈</RainbowButton>
        </div>
        <div className="relative">
          <Image
            src="/logo.png"
            alt="Hero"
            width={500}
            height={500}
            className="mx-auto object-cover rounded-lg"
          />
        </div>
      </div>
    </section>
  );
};

export default HeroSection;
