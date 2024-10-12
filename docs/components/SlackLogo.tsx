"use client";
import React from "react";
import Image from "next/image";

const CustomLogo = () => {
  return (
    <div className="flex items-center space-x-2">
      <Image src="/slack.svg" alt="slack Logo" width={24} height={24} />
    </div>
  );
};

export default CustomLogo;
