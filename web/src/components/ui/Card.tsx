interface CardProps {
  children: React.ReactNode;
  className?: string;
  hover?: boolean;
  padding?: boolean;
}

export function Card({ children, className = '', hover = false, padding = true }: CardProps) {
  return (
    <div
      className={`bg-white border border-gray-200 rounded-lg ${
        padding ? 'p-5' : ''
      } ${
        hover ? 'hover:shadow-sm hover:border-gray-300 transition-all duration-150' : ''
      } ${className}`}
    >
      {children}
    </div>
  );
}

export function CardHeader({ children, className = '' }: { children: React.ReactNode; className?: string }) {
  return (
    <div className={`flex items-center justify-between mb-4 ${className}`}>
      {children}
    </div>
  );
}

export function CardTitle({ children, className = '' }: { children: React.ReactNode; className?: string }) {
  return (
    <h3 className={`text-sm font-semibold text-gray-900 ${className}`}>
      {children}
    </h3>
  );
}

export default Card;
