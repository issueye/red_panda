import { Component } from 'react';
import { AlertTriangle, RefreshCw } from 'lucide-react';
import { Button } from './button.jsx';

/**
 * Lightweight class boundary so one settings tab cannot white-screen the shell.
 */
export class ErrorBoundary extends Component {
  constructor(props) {
    super(props);
    this.state = { error: null };
    this.handleReset = this.handleReset.bind(this);
  }

  static getDerivedStateFromError(error) {
    return { error };
  }

  componentDidCatch(error, info) {
    this.props.onError?.(error, info);
  }

  componentDidUpdate(prevProps) {
    if (prevProps.resetKey !== this.props.resetKey && this.state.error) {
      this.setState({ error: null });
    }
  }

  handleReset() {
    this.setState({ error: null });
    this.props.onReset?.();
  }

  render() {
    const { error } = this.state;
    if (!error) {
      return this.props.children;
    }

    const title = this.props.title || '此区域暂时无法显示';
    const message = error?.message || String(error);
    const Fallback = this.props.fallback;

    if (typeof Fallback === 'function') {
      return (
        <Fallback
          error={error}
          message={message}
          onReset={this.handleReset}
          title={title}
        />
      );
    }

    return (
      <div className="ui-error-boundary" data-testid="error-boundary" role="alert">
        <div className="ui-error-boundary-icon" aria-hidden="true">
          <AlertTriangle size={18} />
        </div>
        <div className="ui-error-boundary-body">
          <strong>{title}</strong>
          <span>{message}</span>
        </div>
        <Button
          data-testid="error-boundary-retry"
          icon={<RefreshCw size={14} />}
          onClick={this.handleReset}
          variant="soft"
        >
          重试
        </Button>
      </div>
    );
  }
}
