#!/bin/bash

###
# Скрипт установки и первичной настройки golang-monitoring server
###

#source "./newt_colors.sh"

#проверка на запуск от sudo/root
check_sudo(){
if [[ $EUID -ne 0 ]]; then
	echo -e "\\e[31mНеобходисо запустить скрипт от sudo или root\\e[0m"
	exit 1
fi
}

#проверка на наличие whiptail(псевдографика)
check_install_whiptail(){
    if ! command -v whiptail &> /dev/null; then
		echo "whiptail не установлен"
		read -p "Хотите установить whiptail? (y/n): " f3
			if [ "$f3" = "y" ] || [ "$f3" = "Y" ]; then
				if sudo apt update && sudo apt install whiptail -y; then
					echo "whiptail установлен"
				else
					echo -e "\\e[31mwhiptail не установлен, попробуйте установить вручную\\e[0m"
					exit 1
				fi
			else
				echo -e "\\e[33mУстановка отменена\\e[0m"
				echo -e "\\e[31mбез whiptail скрипт не запустится неисправно\\e[0m"
				exit 1
			fi
    fi
}

show_main_menu() {
check_sudo
check_install_whiptail

apt install golang -y


if whiptail --title "Выбор установки" --yesno "начать установку monitoring server?" 10 40; then
	whiptail --title "Уведомление" --msgbox "Запуск установки" 10 40
    else
	echo "Пользователь отказался от установки, завершение..."
	exit 1

fi

systemctl stop agent-server
(

echo "XXX"
echo 30
echo "Установка golang"
echo "XXX"

sleep 1
echo "XXX"
echo 60
echo "Копирование фалов"
echo "XXX"

chmod +x ./opt/agent-server/agent-server
cp -r ./opt /
sleep 1
echo "XXX"
echo 90
echo "Настройка агента"
echo "XXX"

cp agent-server.service /etc/systemd/system/
sleep 1
echo "XXX"
echo 95
echo "Запуск агента"
echo "XXX"

systemctl start agent-server
sleep 1
echo "XXX"
echo 100
echo "Установка завершена"
echo "XXX"

) | whiptail --title "ustanovka" --gauge "pocess instaling" 10 70 0
}
show_main_menu
sleep 1
echo http://localhost:8080/
