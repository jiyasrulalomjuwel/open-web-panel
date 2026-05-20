import { motion } from 'framer-motion';

interface CardProps {
  children: React.ReactNode;
  className?: string;
  padding?: boolean;
  hover?: boolean;
  onClick?: () => void;
}

export default function Card({ children, className = '', padding = true, hover = false, onClick }: CardProps) {
  const Component = onClick ? motion.button : motion.div;
  return (
    <Component
      onClick={onClick}
      initial={{ opacity: 0, y: 10 }}
      animate={{ opacity: 1, y: 0 }}
      className={`bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl shadow-sm ${
        padding ? 'p-5' : ''
      } ${hover ? 'hover:shadow-md hover:border-gray-300 dark:hover:border-gray-600 transition-all duration-200 cursor-pointer' : ''} ${
        className
      }`}
    >
      {children}
    </Component>
  );
}
