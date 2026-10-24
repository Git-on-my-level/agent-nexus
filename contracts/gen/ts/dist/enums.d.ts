/** agent_notification_status values (strict), in contract order. */
export declare const AGENT_NOTIFICATION_STATUS_VALUES: readonly string[];
/** artifact_kind values (strict), in contract order. */
export declare const ARTIFACT_KIND_VALUES: readonly string[];
/** backing_scope values (strict), in contract order. */
export declare const BACKING_SCOPE_VALUES: readonly string[];
/** board_column_key values (strict), in contract order. */
export declare const BOARD_COLUMN_KEY_VALUES: readonly string[];
/** card_resolution values (strict), in contract order. */
export declare const CARD_RESOLUTION_VALUES: readonly string[];
/** derived_agent_state values (strict), in contract order. */
export declare const DERIVED_AGENT_STATE_VALUES: readonly string[];
/** event_group values (strict), in contract order. */
export declare const EVENT_GROUP_VALUES: readonly string[];
/** event_type values (strict), in contract order. */
export declare const EVENT_TYPE_VALUES: readonly string[];
/** host_bridge_job values (strict), in contract order. */
export declare const HOST_BRIDGE_JOB_VALUES: readonly string[];
/** human_attention_response_outcome values (strict), in contract order. */
export declare const HUMAN_ATTENTION_RESPONSE_OUTCOME_VALUES: readonly string[];
/** lifecycle_state values (strict), in contract order. */
export declare const LIFECYCLE_STATE_VALUES: readonly string[];
/** priority values (strict), in contract order. */
export declare const PRIORITY_VALUES: readonly string[];
/** Accepted legacy priority spellings and the canonical value each folds to. */
export declare const PRIORITY_ALIASES: Readonly<Record<string, string>>;
/** Trim and lower-case value, then fold an alias; "" when it is neither a priority value nor an alias. */
export declare function normalizePriority(value: unknown): string;
/** run_liveness values (strict), in contract order. */
export declare const RUN_LIVENESS_VALUES: readonly string[];
/** run_state values (strict), in contract order. */
export declare const RUN_STATE_VALUES: readonly string[];
/** work_item_risk values (strict), in contract order. */
export declare const WORK_ITEM_RISK_VALUES: readonly string[];
