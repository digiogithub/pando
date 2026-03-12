import { ReadableSpan } from '@opentelemetry/sdk-trace-base';
import { Span } from './types';
export declare function getReadableSpanTransformer(projectId: string, resourceFilter?: RegExp | undefined, stringifyArrayAttributes?: boolean): (span: ReadableSpan) => Span;
