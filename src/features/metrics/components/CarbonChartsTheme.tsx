import { createContext, useContext, useEffect, useState } from 'react';
import { useTheme as useChTheme } from '@/components/common/theme-provider';

export type ThemeType = 'white' | 'g90' | 'g100';

type ThemeContextType = {
  theme: ThemeType;
};

// Create context with default dark theme since the app appears to use dark mode
const ThemeContext = createContext<ThemeContextType>({ theme: 'g90' });

export const useTheme = () => useContext(ThemeContext);

/**
 * Provider component that detects system color scheme preference
 * and provides the appropriate Carbon chart theme
 */
export function CarbonChartsThemeProvider({ children }: { children: React.ReactNode }) {
  const { theme: appTheme } = useChTheme();
  const [theme, setTheme] = useState<ThemeType>('g90'); // Default to dark theme

  useEffect(() => {
    // Map app theme to Carbon chart theme
    if (appTheme === 'dark') {
      setTheme('g100');
    } else if (appTheme === 'system') {
      // Check system preference when theme is set to 'system'
      const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
      setTheme(prefersDark ? 'g100' : 'white');
      
      // Listen for system preference changes
      const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)');
      const handleChange = (e: MediaQueryListEvent) => {
        setTheme(e.matches ? 'g100' : 'white');
      };
      
      mediaQuery.addEventListener('change', handleChange);
      return () => {
        mediaQuery.removeEventListener('change', handleChange);
      };
    } else {
      // Light theme
      setTheme('white');
    }
  }, [appTheme]);

  return (
    <ThemeContext.Provider value={{ theme }}>
      {children}
    </ThemeContext.Provider>
  );
}

// CSS variables for charts to match application theming
export const chartColorVariables = `
  :root {
    --chart-1: 222, 80%, 60%;  /* blue */
    --chart-2: 133, 70%, 50%;  /* green */
    --chart-3: 350, 80%, 60%;  /* red */
    --chart-4: 30, 80%, 60%;   /* orange */
    --chart-5: 270, 70%, 60%;  /* purple */
    --chart-6: 180, 70%, 50%;  /* teal */
    --chart-7: 60, 80%, 60%;   /* yellow */
    --chart-8: 310, 70%, 60%;  /* pink */
  }
  
  .carbon-theme {
    transition: background-color 0.3s ease;
  }
`;

export default function CarbonChartsTheme() {
  return null; // This component doesn't render anything
} 