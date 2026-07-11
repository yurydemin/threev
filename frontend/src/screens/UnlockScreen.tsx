import { useState } from 'react';
import { Lock } from 'lucide-react';
import { Button } from '../components/ui/Button';
import { Input } from '../components/ui/Input';
import { unlock } from '../lib/wails/appsettings';
import { toast } from '../lib/toast';

export interface UnlockScreenProps {
  onUnlocked: () => void;
}

/**
 * "Приложение заблокировано" screen, per Stage 4 Block I.
 *
 * Rendered by `App.tsx`'s boot gate BEFORE the normal `Screen` union takes
 * over — no `Sidebar`, this is a deliberately exclusive screen the user
 * can't navigate away from except by unlocking.
 *
 * `Unlock` resolves `false` (not a rejection) on a wrong password — that's
 * a normal, expected outcome shown inline, distinct from an actual call
 * failure (network/binding issue), which is reported via `toast.error`
 * in addition to an inline message since it's not something retyping the
 * password alone would fix.
 *
 * There's no "Забыли пароль?" recovery link: the backend has no recovery
 * mechanism, and a dead-end link would be actively misleading.
 */
export function UnlockScreen({ onUnlocked }: UnlockScreenProps) {
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(false);

  async function handleUnlock() {
    if (!password) return;
    setIsLoading(true);
    setError(null);
    try {
      const ok = await unlock(password);
      if (ok) {
        onUnlocked();
      } else {
        setError('Неверный пароль');
      }
    } catch (err) {
      console.error('[UnlockScreen] unlock failed:', err);
      setError('Не удалось выполнить разблокировку');
      toast.error('Не удалось выполнить разблокировку');
    } finally {
      setIsLoading(false);
    }
  }

  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-4 px-6 text-center">
      <Lock className="h-12 w-12 text-accent" aria-hidden="true" />
      <div className="flex flex-col gap-1.5">
        <h1 className="text-xl font-semibold text-fg-primary">Приложение заблокировано</h1>
        <p className="text-sm text-fg-secondary">
          Введите мастер-пароль для доступа к сохранённым подключениям
        </p>
      </div>

      <form
        className="flex w-full max-w-xs flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          void handleUnlock();
        }}
      >
        <Input
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          error={error ?? undefined}
          autoFocus
          placeholder="Мастер-пароль"
        />
        <Button type="submit" variant="primary" isLoading={isLoading} disabled={!password}>
          Разблокировать
        </Button>
      </form>

      <p className="max-w-xs text-xs text-fg-muted">
        Если вы забыли пароль — восстановление недоступно; сохранённые подключения останутся зашифрованными
      </p>
    </div>
  );
}
