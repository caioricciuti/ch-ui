"use client";

import React, { useMemo } from "react";

interface ColourBlogHeaderProps {
  title: string;
  date: string;
}

const ColourBlogHeader: React.FC<ColourBlogHeaderProps> = ({ title, date }) => {
  const generateRandomColor = () => `hsl(${Math.random() * 360}, 70%, 60%)`;

  const gradientStyle = useMemo(() => {
    const color1 = generateRandomColor();
    const color2 = generateRandomColor();
    const color3 = generateRandomColor();
    return {
      background: `
        radial-gradient(circle at 10% 90%, ${color1}, transparent 50%),
        radial-gradient(circle at 90% 10%, ${color2}, transparent 50%),
        radial-gradient(circle at 50% 50%, ${color3}, transparent 70%)
      `,
      opacity: 0.9,
    };
  }, []);

  return (
    <div className="w-full rounded-t-md rounded-b-none overflow-hidden">
      <div className="relative w-full h-26 flex items-end">
        <div
          className="absolute inset-0 rounded-t-lg rounded-b-none"
          style={gradientStyle}
        />
        <div
          className="absolute inset-0"
          style={{
            background:
              "linear-gradient(45deg, rgba(0,0,0,0.6) 0%, rgba(0,0,0,0) 100%)",
          }}
        />
        <div className="relative z-10 p-6">
          <h2 className="text-2xl font-bold mb-2 truncate">{title}</h2>
          <div className="text-sm opacity-80">
            <span>{date}</span>
          </div>
        </div>
      </div>
    </div>
  );
};

export default ColourBlogHeader;
