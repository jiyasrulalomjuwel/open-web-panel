import { motion } from 'framer-motion';

interface ProgressBarProps {
  value: number;
  max?: number;
  label?: string;
  size?: 'sm' | 'md';
  variant?: 'blue' | 'emerald' | 'amber' | 'red';
}

const variants = {
  blue: 'bg-blue-600',
  emerald: 'bg-emerald-600',
  amber: 'bg-amber-500',
  red: 'bg-red-600',
};

export default function ProgressBar({ value, max = 100, label, size = 'md', variant = 'blue' }: ProgressBarProps) {
  const pct = Math.min(Math.round((value / max) * 100), 100);
  return (
    <div className="space-y-1">
      {label && (
        <div className="flex justify-between text-sm">
          <span className="text-gray-600 dark:text-gray-400">{label}</span>
          <span className="text-gray-900 dark:text-gray-100 font-medium">{pct}%</span>
        </div>
      )}
      <div className={`bg-gray-200 dark:bg-gray-700 rounded-full overflow-hidden ${size === 'sm' ? 'h-1.5' : 'h-2.5'}`}>
        <motion.div
          initial={{ width: 0 }}
          animate={{ width: `${pct}%` }}
          transition={{ duration: 0.8, ease: 'easeOut' }}
          className={`h-full rounded-full ${variants[variant]}`}
        />
      </div>
    </div>
  );
}
