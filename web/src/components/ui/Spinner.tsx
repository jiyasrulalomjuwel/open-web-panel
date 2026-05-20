import { motion } from 'framer-motion';

interface SpinnerProps {
  size?: number;
  className?: string;
  text?: string;
}

export default function Spinner({ size = 24, className = '', text }: SpinnerProps) {
  return (
    <div className={`flex flex-col items-center justify-center gap-2 ${className}`}>
      <motion.div
        className="border-2 border-gray-200 border-t-blue-600 dark:border-gray-700 dark:border-t-blue-400 rounded-full"
        style={{ width: size, height: size }}
        animate={{ rotate: 360 }}
        transition={{ duration: 0.8, repeat: Infinity, ease: 'linear' }}
      />
      {text && <p className="text-sm text-gray-500 dark:text-gray-400">{text}</p>}
    </div>
  );
}
