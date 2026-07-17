import { motion } from 'framer-motion';

interface SpinnerProps {
  size?: number;
  className?: string;
  text?: string;
}

export function Spinner({ size = 24, className = '', text }: SpinnerProps) {
  return (
    <div className={`flex flex-col items-center justify-center gap-3 ${className}`}>
      <motion.div
        className="border-2 border-gray-200 border-t-purple-600 rounded-full"
        style={{ width: size, height: size }}
        animate={{ rotate: 360 }}
        transition={{ duration: 0.6, repeat: Infinity, ease: 'linear' }}
      />
      {text && <p className="text-sm text-gray-500">{text}</p>}
    </div>
  );
}

export default Spinner;
