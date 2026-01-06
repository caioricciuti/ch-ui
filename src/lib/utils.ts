import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export function genTabId() {
  return `tab-${Math.random().toString(36).substring(2, 15)}`;
}

export function formatBytes(bytes: number) {
  if (!bytes) return "";
  if (bytes === 0) return "0 Bytes";
  const k = 1024;
  const sizes = ["Bytes", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + " " + sizes[i];
}

export function formatDate(date: Date, format: string) {
  if (!date) return "";
  return date.toLocaleString("en-US", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  });
}

export function formatNumber(number: number) {
  if (!number) return "";
  return number.toLocaleString("en-US", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  });
}


export function generateRandomPassword(length: number = 16) {
  const lowercase = "abcdefghijklmnopqrstuvwxyz";
  const uppercase = "ABCDEFGHIJKLMNOPQRSTUVWXYZ";
  const numbers = "0123456789";
  const special = "!@#$%^&*()_+-=[]{}|;:,.<>?";

  const allChars = lowercase + uppercase + numbers + special;

  // Ensure at least one of each required type
  let password = [
    lowercase[Math.floor(Math.random() * lowercase.length)],
    uppercase[Math.floor(Math.random() * uppercase.length)],
    numbers[Math.floor(Math.random() * numbers.length)],
    special[Math.floor(Math.random() * special.length)],
  ];

  // Fill the rest randomly
  for (let i = 4; i < length; i++) {
    password.push(allChars[Math.floor(Math.random() * allChars.length)]);
  }

  // Shuffle the password
  return password.sort(() => 0.5 - Math.random()).join('');
}

export function redactSecrets(query: string): string {
  if (!query) return "";
  let redacted = query;
  // Redact IDENTIFIED BY/WITH '...'
  redacted = redacted.replace(/(IDENTIFIED\s+(?:WITH\s+[a-zA-Z0-9_]+\s+)?BY\s+)'[^']+'/gi, "$1'******'");
  // Redact PASSWORD '...'
  redacted = redacted.replace(/(PASSWORD\s+)'[^']+'/gi, "$1'******'");
  return redacted;
}
