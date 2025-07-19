import { Component, Input } from '@angular/core';

@Component({
  selector: 'app-message',
  templateUrl: './message.html',
  styleUrls: ['./message.css']
})
export class MessageComponent {
  @Input() message: any;
}
