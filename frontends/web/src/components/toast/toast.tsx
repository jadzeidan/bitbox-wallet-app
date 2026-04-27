// SPDX-License-Identifier: Apache-2.0

import { ReactNode, createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { Message } from '@/components/message/message';
import { TMessageTypes } from '@/utils/types';
import style from './Toast.module.css';

type TToastProps = {
  type?: TMessageTypes;
  // Deprecated prop kept for compatibility with the existing callsites.
  theme?: TMessageTypes;
  className?: string;
  children: ReactNode;
};

type TToastItem = {
  id: number;
  duration: number;
  message: ReactNode;
  type: TMessageTypes;
};

type TShowToast = {
  duration?: number;
  message: ReactNode;
  type?: TMessageTypes;
};

type TToastContext = {
  clearToasts: () => void;
  hideToast: (id: number) => void;
  showToast: (toast: TShowToast) => number;
};

type TToastProviderProps = {
  children: ReactNode;
};

const DEFAULT_DURATION_MS = 5000;
const ToastContext = createContext<TToastContext | undefined>(undefined);

let nextToastID = 1;

export const Toast = ({ type, theme, className = '', children }: TToastProps) => {
  const resolvedType = type || theme || 'info';
  return (
    <Message type={resolvedType} className={`${style.toast || ''} ${className || ''}`.trim()}>
      <div className={style.content}>{children}</div>
    </Message>
  );
};

export const ToastProvider = ({ children }: TToastProviderProps) => {
  const [toasts, setToasts] = useState<TToastItem[]>([]);
  const timeoutIDs = useRef<Record<number, ReturnType<typeof setTimeout>>>({});

  const hideToast = useCallback((id: number) => {
    const timeoutID = timeoutIDs.current[id];
    if (timeoutID) {
      clearTimeout(timeoutID);
      delete timeoutIDs.current[id];
    }
    setToasts(prevToasts => prevToasts.filter(toast => toast.id !== id));
  }, []);

  const clearToasts = useCallback(() => {
    Object.values(timeoutIDs.current).forEach(clearTimeout);
    timeoutIDs.current = {};
    setToasts([]);
  }, []);

  const showToast = useCallback((toast: TShowToast) => {
    const id = nextToastID;
    nextToastID += 1;

    const nextToast: TToastItem = {
      duration: toast.duration ?? DEFAULT_DURATION_MS,
      id,
      message: toast.message,
      type: toast.type ?? 'info',
    };

    setToasts(prevToasts => [...prevToasts, nextToast]);

    if (nextToast.duration > 0) {
      timeoutIDs.current[id] = setTimeout(() => {
        hideToast(id);
      }, nextToast.duration);
    }
    return id;
  }, [hideToast]);

  useEffect(() => {
    return () => {
      Object.values(timeoutIDs.current).forEach(clearTimeout);
    };
  }, []);

  const value = useMemo(() => ({
    clearToasts,
    hideToast,
    showToast,
  }), [clearToasts, hideToast, showToast]);

  return (
    <ToastContext.Provider value={value}>
      {children}
      {toasts.length > 0 && (
        <div className={style.viewport} aria-live="polite" role="status">
          {toasts.map(({ id, message, type }) => (
            <Toast key={id} className={style.toastItem} type={type}>
              {message}
            </Toast>
          ))}
        </div>
      )}
    </ToastContext.Provider>
  );
};

export const useToast = () => {
  const context = useContext(ToastContext);
  if (!context) {
    throw new Error('useToast must be used within ToastProvider.');
  }
  return context;
};
