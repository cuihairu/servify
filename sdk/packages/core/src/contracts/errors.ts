export type ServifyErrorCode =
  | 'transport_unavailable'
  | 'transport_timeout'
  | 'transport_disconnected'
  | 'auth_failed'
  | 'auth_refresh_required'
  | 'capability_unsupported'
  | 'capture_denied'
  | 'capture_unavailable'
  | 'capture_already_active'
  | 'api_error'
  | 'validation_error'
  | 'unknown_error';

export interface ServifyErrorOptions {
  code: ServifyErrorCode;
  cause?: unknown;
  details?: Record<string, unknown>;
  retryable?: boolean;
}

export class ServifyError extends Error {
  readonly code: ServifyErrorCode;
  readonly cause?: unknown;
  readonly details?: Record<string, unknown>;
  readonly retryable: boolean;

  constructor(message: string, options: ServifyErrorOptions) {
    super(message);
    this.name = 'ServifyError';
    this.code = options.code;
    this.cause = options.cause;
    this.details = options.details;
    this.retryable = options.retryable ?? false;
  }
}
