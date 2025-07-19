import { Injectable } from '@angular/core';

@Injectable({
  providedIn: 'root'
})
export class NotificationService {

  constructor() { }

  requestPermission() {
    if (!("Notification" in window)) {
      console.log("This browser does not support desktop notification");
    }
    else if (Notification.permission !== "denied") {
      Notification.requestPermission().then(function (permission) {
        if (permission === "granted") {
          console.log("Notification permission granted.");
        }
      });
    }
  }

  showNotification(title: string, options?: NotificationOptions) {
    if (Notification.permission === "granted") {
      new Notification(title, options);
    }
  }

  playSound() {
    const audio = new Audio('assets/notification.mp3');
    audio.play();
  }
}
