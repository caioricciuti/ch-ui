import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export const generateRandomPassword = () => {
  const length = 16;
  const charset = {
    uppercase: "ABCDEFGHIJKLMNOPQRSTUVWXYZ",
    lowercase: "abcdefghijklmnopqrstuvwxyz",
    numbers: "0123456789",
    symbols: "!@#$%^&*()_+-=[]{}|;:,.<>?",
  };

  let password = "";
  // Ensure at least one of each type
  password +=
    charset.uppercase[Math.floor(Math.random() * charset.uppercase.length)];
  password +=
    charset.lowercase[Math.floor(Math.random() * charset.lowercase.length)];
  password +=
    charset.numbers[Math.floor(Math.random() * charset.numbers.length)];
  password +=
    charset.symbols[Math.floor(Math.random() * charset.symbols.length)];

  // Fill the rest randomly
  const allChars = Object.values(charset).join("");
  for (let i = password.length; i < length; i++) {
    password += allChars[Math.floor(Math.random() * allChars.length)];
  }
  // Shuffle the password
  return password
    .split("")
    .sort(() => Math.random() - 0.5)
    .join("");
};

export const genTabId = () => {
  const timestamp = Date.now().toString();
  const randomStr = Math.random().toString(36).substring(2);
  return timestamp + randomStr.slice(0, 26 - timestamp.length);
};



export const formatBytes = (bytes: string | number | null | undefined): string => {
  const numBytes = Number(bytes);
  if (!bytes || isNaN(numBytes) || numBytes === 0) return "N/A";
  const k = 1024;
  const sizes = ["Bytes", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(numBytes) / Math.log(k));
  return parseFloat((numBytes / Math.pow(k, i)).toFixed(2)) + " " + sizes[i];
};


export const formatDate = (date: string | number | Date): string => {
  if (!date) return "N/A";
  return new Date(date).toLocaleString();
};

export const formatNumber = (
  value: number | string | null | undefined
): string => {
  if (value === null || value === undefined) return "N/A";
  const num = typeof value === "string" ? parseFloat(value) : value;
  if (isNaN(num)) return "N/A";
  return num.toLocaleString();
};

export const calculateEfficiency = (
  total: number,
  lifetime: number
): number => {
  if (lifetime === 0) return 0;
  return (total / lifetime) * 100;
};

export const getEfficiencyColor = (value: number): string => {
  if (value >= 90) return "text-red-500";
  if (value >= 70) return "text-yellow-500";
  return "text-green-500";
};
