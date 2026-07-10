/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        bg: {
          primary: 'var(--bg-primary)',
          secondary: 'var(--bg-secondary)',
          tertiary: 'var(--bg-tertiary)',
          elevated: 'var(--bg-elevated)',
        },
        fg: {
          primary: 'var(--fg-primary)',
          secondary: 'var(--fg-secondary)',
          muted: 'var(--fg-muted)',
        },
        accent: {
          DEFAULT: 'var(--accent)',
          hover: 'var(--accent-hover)',
          subtle: 'var(--accent-subtle)',
        },
        success: 'var(--success)',
        warning: 'var(--warning)',
        danger: {
          DEFAULT: 'var(--danger)',
          hover: 'var(--danger-hover)',
          subtle: 'var(--danger-subtle)',
        },
        border: {
          DEFAULT: 'var(--border)',
          subtle: 'var(--border-subtle)',
        },
      },
      fontFamily: {
        sans: ['-apple-system', 'BlinkMacSystemFont', 'Segoe UI', 'Roboto', 'Helvetica Neue', 'Arial', 'sans-serif'],
        mono: ['JetBrains Mono', 'SF Mono', 'Menlo', 'Monaco', 'monospace'],
      },
      fontSize: {
        '2xs': '11px',
      },
      spacing: {
        sidebar: '240px',
        header: '48px',
        'bottom-panel': '180px',
        row: '36px',
      },
      borderRadius: {
        DEFAULT: '6px',
        sm: '4px',
      },
      transitionDuration: {
        fast: '150ms',
        normal: '250ms',
      },
      keyframes: {
        'toast-in': {
          from: { transform: 'translateX(16px)', opacity: '0' },
          to: { transform: 'translateX(0)', opacity: '1' },
        },
      },
      animation: {
        'pulse-slow': 'pulse 1.5s ease-in-out infinite',
        'toast-in': 'toast-in 250ms ease-out',
      },
    },
  },
  plugins: [],
}
