import { useEffect, useState, type ButtonHTMLAttributes, type ReactNode } from 'react';
import { cx } from '../lib/format';

export type ButtonVariant = 'primary' | 'secondary' | 'danger' | 'danger-fill' | 'ghost';

interface ButtonProps extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'onClick'> {
  variant?: ButtonVariant;
  /** Destructive actions: the first click turns the label into "Confirm?" for 3 seconds. */
  confirm?: boolean;
  onClick?: () => void;
  children: ReactNode;
}

const CONFIRM_MS = 3_000;

export function Button({
  variant = 'secondary',
  confirm = false,
  onClick,
  type = 'button',
  className,
  disabled,
  children,
  ...rest
}: ButtonProps) {
  const [armed, setArmed] = useState(false);

  useEffect(() => {
    if (!armed) return;
    const timer = window.setTimeout(() => setArmed(false), CONFIRM_MS);
    return () => window.clearTimeout(timer);
  }, [armed]);

  const handleClick = () => {
    if (!confirm) {
      onClick?.();
      return;
    }
    if (!armed) {
      setArmed(true);
      return;
    }
    setArmed(false);
    onClick?.();
  };

  return (
    <button {...rest} type={type} className={cx('btn', `btn-${variant}`, className)} disabled={disabled} onClick={handleClick}>
      {confirm && armed ? 'Confirm?' : children}
    </button>
  );
}
