import {useEffect,useRef,useState} from 'react';
import type {IncidentCard,VehicleState,WSEvent} from '../types/fleet';

function fleetWSURL(){
  const protocol=window.location.protocol==='https:'?'wss:':'ws:';
  return `${protocol}//${window.location.host}/ws/fleet`;
}

export function useFleetSocket(){
  const [vehicles,setVehicles]=useState<VehicleState[]>([]);
  const [incidents,setIncidents]=useState<IncidentCard[]>([]);
  const [connected,setConnected]=useState(false);
  const retry=useRef<number|undefined>(undefined);

  useEffect(()=>{
    let ws:WebSocket|undefined;
    let dead=false;

    const connect=()=>{
      ws=new WebSocket(fleetWSURL());
      ws.onopen=()=>setConnected(true);
      ws.onclose=()=>{
        setConnected(false);
        if(!dead) retry.current=window.setTimeout(connect,1500);
      };
      ws.onmessage=e=>{
        const msg=WSEventSafe(e.data);
        if(!msg) return;

        switch(msg.type){
          case 'INIT':
            setVehicles(msg.payload.vehicles);
            setIncidents(msg.payload.incidents);
            break;
          case 'VEHICLE_UPDATE':
            setVehicles(current=>{
              const i=current.findIndex(x=>x.unit_id===msg.payload.unit_id);
              if(i<0) return [...current,msg.payload];
              const next=[...current];
              next[i]=msg.payload;
              return next;
            });
            break;
          case 'INCIDENT':
            setIncidents(current=>[
              msg.payload,
              ...current.filter(x=>!(x.unit_id===msg.payload.unit_id&&x.detected_at===msg.payload.detected_at)),
            ].slice(0,30));
            break;
        }
      };
    };

    connect();
    return()=>{
      dead=true;
      if(retry.current!==undefined) window.clearTimeout(retry.current);
      ws?.close();
    };
  },[]);

  return{vehicles,incidents,connected};
}

function WSEventSafe(x:string):WSEvent|null{
  try{
    const parsed=JSON.parse(x) as {type?:string};
    if(parsed.type==='INIT'||parsed.type==='VEHICLE_UPDATE'||parsed.type==='INCIDENT'){
      return parsed as WSEvent;
    }
    return null;
  }catch{
    return null;
  }
}
