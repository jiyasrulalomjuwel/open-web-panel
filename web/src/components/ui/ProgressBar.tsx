import { motion } from 'framer-motion';

type BarVariant = 'purple' | 'blue' | 'emerald' | 'amber' | 'red' | 'default' | 'warning' | 'danger';

interface ProgressBarProps {
  value: number;
  max?: number;
  className?: string;
  size?: 'sm' | 'md' | 'lg';
  variant?: BarVariant;
}

const colorMap: Record<BarVariant, string> = {
  purple: 'bg-purple-500',
  blue: 'bg-blue-500',
  emerald: 'bg-emerald-500',
  amber: 'bg-amber-500',
  red: 'bg-red-500',
  default: 'bg-purple-500',
  warning: 'bg-amber-500',
  danger: 'bg-red-500',
};

export function ProgressBar({ value, max = 100, className = '', size = 'sm', variant = 'purple' }: ProgressBarProps) {
  const pct = Math.min(100, Math.round((value / max) * 100));
  const h = size === 'md' ? 'h-2.5' : size === 'lg' ? 'h-3' : 'h-1.5';

  return (
    <div className={`${h} bg-gray-100 rounded-full overflow-hidden ${className}`}>
      <motion.div
        initial={{ width: 0 }}
        animate={{ width: `${pct}%` }}
        transition={{ duration: 0.5, ease: 'easeOut' }}
        className={`h-full rounded-full ${colorMap[variant]}`}
      />
    </div>
  );
}

export default ProgressBar;
