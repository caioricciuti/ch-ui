"use client";

import React from "react";
import Image from "next/image";

const CustomLogo = () => {
  return (
    <div className="flex items-center space-x-2">
      <Image src="/logo.png" alt="CH-UI Logo" width={32} height={32} />
      <span className="font-bold text-xl">CH-UI</span>
    </div>
  );
};

export default CustomLogo;
