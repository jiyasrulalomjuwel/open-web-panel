/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      fontFamily: {
        sans: ['Inter', '-apple-system', 'BlinkMacSystemFont', 'Segoe UI', 'Roboto', 'sans-serif'],
      },
      colors: {
        primary: {
          50: '#EFF6FF',
          100: '#DBEAFE',
          200: '#BFDBFE',
          300: '#93C5FD',
          400: '#60A5FA',
          500: '#3B82F6',
          600: '#2563EB',
          700: '#1D4ED8',
        },
        surface: '#F7F8FC',
        'border-subtle': '#ECEEF4',
      },
      borderRadius: {
        card: '18px',
      },
      boxShadow: {
        'soft': '0 1px 2px rgba(16,24,40,0.05)',
        'card': '0 1px 2px rgba(16,24,40,0.05), 0 4px 16px rgba(16,24,40,0.04)',
        'card-hover': '0 1px 3px rgba(16,24,40,0.08), 0 6px 24px rgba(16,24,40,0.06)',
        'dropdown': '0 4px 16px rgba(16,24,40,0.08), 0 8px 32px rgba(16,24,40,0.06)',
      },
      animation: {
        'fade-in': 'fadeIn 0.2s ease-out',
        'slide-up': 'slideUp 0.2s ease-out',
      },
      keyframes: {
        fadeIn: {
          '0%': { opacity: '0' },
          '100%': { opacity: '1' },
        },
        slideUp: {
          '0%': { opacity: '0', transform: 'translateY(8px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' },
        },
      },
    },
  },
  plugins: [],
};
