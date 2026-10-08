import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";

// HUI-2627 finish-r1:错误态不只是文案,还必须给一条恢复动作。
// 只包设计系统组件(Alert/Button),不引入裸控件或裸 hex。
export function ErrorBanner({ message, onRetry }: { message: string; onRetry: () => void }) {
  if (!message) return null;
  return (
    <Alert tone="danger" role="alert">
      {message}
      <Button type="button" size="sm" data-testid="error-retry" onClick={onRetry}>
        重试
      </Button>
    </Alert>
  );
}
