import { describe, expect, it } from 'vitest';

import { TRANSLATION_METADATA_KEYS, readMessageTranslation } from './translation';

describe('translation metadata contract (Phase 0.5)', () => {
  it('exposes the reserved metadata key names', () => {
    expect(TRANSLATION_METADATA_KEYS.text).toBe('translation');
    expect(TRANSLATION_METADATA_KEYS.lang).toBe('translation_lang');
  });

  it('reads translated text and language from message metadata', () => {
    expect(readMessageTranslation({ translation: 'Hello', translation_lang: 'en', source: 'admin' })).toEqual({
      text: 'Hello',
      lang: 'en',
    });
  });

  it('returns null when metadata or the translation key is missing', () => {
    expect(readMessageTranslation(undefined)).toBeNull();
    expect(readMessageTranslation(null)).toBeNull();
    expect(readMessageTranslation({ source: 'admin' })).toBeNull();
    expect(readMessageTranslation({ translation: '' })).toBeNull();
  });

  it('tolerates a missing language key (lang resolves to empty string)', () => {
    expect(readMessageTranslation({ translation: 'Hello' })).toEqual({ text: 'Hello', lang: '' });
  });
});
