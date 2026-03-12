import { Metadata } from '@grpc/grpc-js';
export interface Span {
    name?: string;
    spanId?: string;
    parentSpanId?: string;
    displayName?: TruncatableString;
    startTime?: Timestamp;
    endTime?: Timestamp;
    attributes?: Attributes;
    stackTrace?: StackTrace;
    timeEvents?: TimeEvents;
    links?: Links;
    status?: Status;
    sameProcessAsParentSpan?: BoolValue;
    childSpanCount?: number;
    spanKind?: SpanKind;
}
export interface Timestamp {
    seconds: number;
    nanos: number;
}
export interface AttributeMap {
    [key: string]: AttributeValue;
}
export interface Attributes {
    attributeMap: AttributeMap;
    droppedAttributesCount?: number;
}
export interface AttributeValue {
    boolValue?: boolean;
    intValue?: string;
    stringValue?: TruncatableString;
}
export interface TruncatableString {
    value?: string;
    truncatedByteCount?: number;
}
export interface Links {
    droppedLinksCount?: number;
    link?: Link[];
}
export interface Link {
    attributes?: Attributes;
    spanId?: string;
    traceId?: string;
    type?: LinkType;
}
export interface StackTrace {
    stackFrames?: StackFrames;
    stackTraceHashId?: string;
}
export interface StackFrames {
    droppedFramesCount?: number;
    frame?: StackFrame[];
}
export interface StackFrame {
    columnNumber?: string;
    fileName?: TruncatableString;
    functionName?: TruncatableString;
    lineNumber?: string;
    loadModule?: Module;
    originalFunctionName?: TruncatableString;
    sourceVersion?: TruncatableString;
}
export interface Module {
    buildId?: TruncatableString;
    module?: TruncatableString;
}
export interface Status {
    /** gRPC status code */
    code?: Code;
    message?: string;
}
export interface TimeEvents {
    droppedAnnotationsCount?: number;
    droppedMessageEventsCount?: number;
    timeEvent?: TimeEvent[];
}
export interface TimeEvent {
    annotation?: Annotation;
    time?: Timestamp;
    messageEvent?: MessageEvent;
}
export interface Annotation {
    attributes?: Attributes;
    description?: TruncatableString;
}
export interface MessageEvent {
    id?: string;
    type?: Type;
    compressedSizeBytes?: string;
    uncompressedSizeBytes?: string;
}
export declare enum Type {
    TYPE_UNSPECIFIED = 0,
    SENT = 1,
    RECEIVED = 2
}
export declare enum LinkType {
    UNSPECIFIED = 0,
    CHILD_LINKED_SPAN = 1,
    PARENT_LINKED_SPAN = 2
}
/**
 * A protobuf boolean
 */
export interface BoolValue {
    value: boolean;
}
export interface NamedSpans {
    name: string;
    spans: Span[];
}
export interface TraceService {
    BatchWriteSpans: (call: NamedSpans, metadata: Metadata, callback: Function) => void;
}
/**
 * A google.rpc.Code
 */
export declare enum Code {
    OK = 0,
    UNKNOWN = 2
}
/**
 * See https://github.com/googleapis/googleapis/blob/8cd4d12c0a02872469176659603451d84c0fbee7/google/devtools/cloudtrace/v2/trace.proto#L182
 */
export declare enum SpanKind {
    SPAN_KIND_UNSPECIFIED = 0,
    INTERNAL = 1,
    SERVER = 2,
    CLIENT = 3,
    PRODUCER = 4,
    CONSUMER = 5
}
