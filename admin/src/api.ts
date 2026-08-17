export type User = { id: string; public_id:number; nickname:string; username: string; email?: string | null; status: string; must_change_password: boolean; roles: string[]; created_at?: string; last_login_at?: string | null };
export type Role = { id: string; key: string; name: string; description: string; is_system: boolean; permissions: string[]; member_count: number };
export type Permission = { id: string; key: string; name: string; description: string };
export type Candidate = { save_type?: number; save_id: number; name: string; english: string; slots: number; rarity?: number; combat?: number; gender?: number; confirmed: boolean };
export type NamedID = { id: number; name: string; english: string };
export type PiecePayload = { save_type: number; save_id: number; decorations: number[] };
export type LoadoutPayload = { schema:"MH_LOADOUT";schema_version:1;game:"mh3g";data_version:string;name:string;gender:"male"|"female";weapon:PiecePayload;armor:{head:PiecePayload;chest:PiecePayload;arms:PiecePayload;waist:PiecePayload;legs:PiecePayload};charm:{class_id:number;slots:number;skill1_id:number;skill1_points:number;skill2_id:number;skill2_points:number;decorations:number[]} };
export type Diagnostic = { code:string;field:string;message:string };
export type Summary = { base_defense:number;max_defense:number;fire_res:number;water_res:number;ice_res:number;thunder_res:number;dragon_res:number;total_slots:number;used_slots:number;skills:Array<{skill_tree_id:number;name:string;points:number;active_skill?:string}>;diagnostics:Diagnostic[] };
export type Loadout = { id:string;owner_user_id:string;owner_public_id:number;owner_nickname:string;owner_username:string;name:string;remark:string;status:string;version:number;data_version:string;is_legal:boolean;like_count:number;liked_by_me:boolean;risk_summary:Summary;payload?:LoadoutPayload;updated_at:string };

let csrfToken=sessionStorage.getItem("mhed-csrf")??"";
export function setCSRF(value:string){csrfToken=value;if(value)sessionStorage.setItem("mhed-csrf",value);else sessionStorage.removeItem("mhed-csrf")}
export class APIError extends Error{constructor(public status:number,public code:string,message:string){super(message)}}
export async function api<T>(path:string,init:RequestInit={}):Promise<T>{const headers=new Headers(init.headers);if(init.body)headers.set("Content-Type","application/json");const method=(init.method??"GET").toUpperCase();if(!["GET","HEAD","OPTIONS"].includes(method)&&csrfToken)headers.set("X-CSRF-Token",csrfToken);const response=await fetch(`/api/v1${path}`,{...init,headers,credentials:"include"});if(response.status===204)return undefined as T;const body=await response.json().catch(()=>({}));if(!response.ok){const error=body.error??{};throw new APIError(response.status,error.code??"REQUEST_FAILED",error.message??`请求失败 (${response.status})`)}if(body.csrf_token)setCSRF(body.csrf_token);return body as T}
