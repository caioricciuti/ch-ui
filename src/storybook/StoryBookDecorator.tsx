import { ThemeProvider } from "@/components/theme-provider";

export default function StoryBookDecorator({ children }: { children: React.ReactNode }) {
  return (
    <ThemeProvider defaultTheme="dark" storageKey="vite-ui-theme">
      <div className="p-4 border rounded-lg">
        {children}
      </div>
    </ThemeProvider>
  );
}