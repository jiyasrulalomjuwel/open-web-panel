import { FileText } from 'lucide-react';
import { Button } from './Button';

interface EmptyStateProps {
  icon?: React.ReactNode;
  title: string;
  message?: string;
  actionLabel?: string;
  onAction?: () => void;
}

export function EmptyState({ icon, title, message, actionLabel, onAction }: EmptyStateProps) {
  return (
    <div className="flex flex-col items-center justify-center py-16 px-4">
      <div className="w-12 h-12 bg-gray-100 rounded-lg flex items-center justify-center mb-4">
        {icon || <FileText className="h-5 w-5 text-gray-400" />}
      </div>
      <h3 className="text-sm font-semibold text-gray-900 mb-1">{title}</h3>
      {message && <p className="text-sm text-gray-500 text-center max-w-sm mb-4">{message}</p>}
      {actionLabel && onAction && (
        <Button variant="primary" size="sm" onClick={onAction}>
          {actionLabel}
        </Button>
      )}
    </div>
  );
}

export default EmptyState;
