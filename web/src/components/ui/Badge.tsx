type BadgeVariant = 'default' | 'success' | 'warning' | 'danger' | 'info' | 'error' | 'neutral';

interface BadgeProps {
  children: React.ReactNode;
  variant?: BadgeVariant;
  className?: string;
  dot?: boolean;
}

const variants: Record<BadgeVariant, string> = {
  default: 'bg-gray-100 text-gray-700',
  success: 'bg-green-50 text-green-700',
  warning: 'bg-amber-50 text-amber-700',
  danger: 'bg-red-50 text-red-700',
  info: 'bg-blue-50 text-blue-700',
  error: 'bg-red-50 text-red-700',
  neutral: 'bg-gray-50 text-gray-600',
};

const dots: Record<string, string> = {
  success: 'bg-green-500',
  warning: 'bg-amber-500',
  error: 'bg-red-500',
  info: 'bg-blue-500',
  neutral: 'bg-gray-500',
  default: 'bg-gray-500',
  danger: 'bg-red-500',
};

export function Badge({ children, variant = 'default', className = '', dot }: BadgeProps) {
  return (
    <span
      className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-md text-xs font-medium ${variants[variant]} ${className}`}
    >
      {dot && <span className={`w-1.5 h-1.5 rounded-full ${dots[variant] || 'bg-gray-500'}`} />}
      {children}
    </span>
  );
}

export default Badge;
