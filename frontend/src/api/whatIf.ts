import type {ReserveWhatIfResponse,TrafficLightWhatIfResponse} from '../types/fleet';

async function postJSON<T>(path:string,body:unknown):Promise<T>{
  const response=await fetch(path,{
    method:'POST',
    headers:{'Content-Type':'application/json'},
    body:JSON.stringify(body),
  });

  if(!response.ok){
    let message=`HTTP ${response.status}`;
    try{
      const payload=await response.json() as {error?:string};
      if(payload.error) message=payload.error;
    }catch{
      // Оставляем HTTP-код, если backend вернул не-JSON ответ.
    }
    throw new Error(message);
  }

  return response.json() as Promise<T>;
}

export function runTrafficLightWhatIf(unitId:number,extensionSeconds=15){
  return postJSON<TrafficLightWhatIfResponse>('/api/v1/what-if/traffic-light',{
    unit_id:unitId,
    extension_s:extensionSeconds,
  });
}

export function runReserveWhatIf(unitId:number,dispatchETASeconds=300){
  return postJSON<ReserveWhatIfResponse>('/api/v1/what-if/reserve',{
    unit_id:unitId,
    dispatch_eta_s:dispatchETASeconds,
  });
}
