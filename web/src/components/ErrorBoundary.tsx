import { Component, ReactNode } from 'react';

interface Props {
  children: ReactNode;
}

interface State {
  hasError: boolean;
  error: Error | null;
}

export class ErrorBoundary extends Component<Props, State> {
  constructor(props: Props) {
    super(props);
    this.state = { hasError: false, error: null };
  }

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, info: any) {
    console.error('ErrorBoundary caught:', error, info);
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className="min-h-screen flex items-center justify-center bg-gray-50 p-8">
          <div className="bg-white rounded-xl border border-red-200 shadow-lg max-w-lg w-full p-6">
            <div className="flex items-center gap-2 mb-3">
              <div className="h-8 w-8 rounded-full bg-red-100 flex items-center justify-center text-red-600 font-bold">!</div>
              <h2 className="font-semibold text-gray-900">Something went wrong</h2>
            </div>
            <p className="text-sm text-gray-500 mb-3">
              An unexpected error occurred. Try refreshing the page.
            </p>
            {this.state.error && (
              <pre className="bg-red-50 border border-red-100 rounded-lg p-3 text-xs text-red-700 overflow-auto max-h-40 mb-4">
                {this.state.error.message}
              </pre>
            )}
            <button
              onClick={() => {
                localStorage.clear();
                window.location.href = '/login';
              }}
              className="w-full py-2 bg-gray-900 text-white rounded-lg text-sm font-medium hover:bg-gray-800"
            >
              Go to Login
            </button>
          </div>
        </div>
      );
    }

    return this.props.children;
  }
}
